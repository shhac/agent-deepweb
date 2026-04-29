package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// ImportHAROptions controls HAR → templates. HAR entries are real
// captured requests (cookies, auth headers, tracking params included),
// so the importer deliberately strips credential-carrying headers and
// drops cookie values before storing. The user re-attaches auth via
// --profile at template-run time.
type ImportHAROptions struct {
	Prefix      string
	Profile     string
	URLContains string // substring filter; empty = import all entries
	Dedupe      bool   // collapse duplicate (method,url,body-shape) → one
}

// ImportHARFile reads a HAR 1.2 JSON file and stores one template.Template per
// log.entries[]. See ImportHAR for the mapping rules.
func ImportHARFile(path string, opts ImportHAROptions) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ImportHAR(data, opts)
}

// ImportHAR parses a HAR document from data. Best-effort: malformed
// entries are skipped with the rest of the file still imported.
func ImportHAR(data []byte, opts ImportHAROptions) ([]string, error) {
	if opts.Prefix == "" {
		return nil, fmt.Errorf("har import requires --prefix")
	}
	var doc harDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse har: %w", err)
	}
	if len(doc.Log.Entries) == 0 {
		return nil, fmt.Errorf("har has no log.entries[] — empty capture?")
	}

	seen := map[string]bool{} // dedupe key
	counters := map[string]int{}
	var imported []string

	for i, e := range doc.Log.Entries {
		if opts.URLContains != "" && !strings.Contains(e.Request.URL, opts.URLContains) {
			continue
		}
		tpl, err := harEntryToTemplate(e, opts, i, counters)
		if err != nil {
			// Skip malformed entries silently — partial imports are
			// still useful, especially on big captures.
			continue
		}
		if opts.Dedupe {
			key := dedupeKey(tpl)
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		if err := template.Store(tpl); err != nil {
			return imported, err
		}
		imported = append(imported, tpl.Name)
	}
	sort.Strings(imported)
	return imported, nil
}

// harDoc is HAR 1.2 viewer's-eye-view — just what we need to emit
// templates. Ignores timing, page refs, server IP, response (templates
// are outbound-only).
type harDoc struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Entries []harEntry `json:"entries"`
}

type harEntry struct {
	Request harRequest `json:"request"`
}

type harRequest struct {
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	Headers     []harNV      `json:"headers"`
	QueryString []harNV      `json:"queryString"`
	PostData    *harPostData `json:"postData"`
}

type harNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPostData struct {
	MimeType string  `json:"mimeType"`
	Text     string  `json:"text"`
	Params   []harNV `json:"params"`
}

// stripHeaders is the allowlist of headers we REMOVE during import.
// Cookies and auth headers are the user's real session; templates that
// get re-run later should pick those up from the profile's jar, not
// carry the stale capture values.
var stripHeaders = map[string]bool{
	"authorization":   true,
	"cookie":          true,
	"set-cookie":      true,
	"x-csrf-token":    true,
	"x-csrftoken":     true,
	"x-xsrf-token":    true,
	"x-api-key":       true, // profile attaches instead
	":authority":      true, // HTTP/2 pseudo-header
	":method":         true,
	":path":           true,
	":scheme":         true,
	"content-length":  true, // auto-computed by the http client
	"host":            true, // auto-set by the http client
	"accept-encoding": true, // transport-level; user shouldn't override
	"user-agent":      true, // profile's UA / per-request --user-agent wins
}

func harEntryToTemplate(e harEntry, opts ImportHAROptions, idx int, counters map[string]int) (template.Template, error) {
	req := e.Request
	if req.URL == "" || req.Method == "" {
		return template.Template{}, fmt.Errorf("entry %d: missing url or method", idx)
	}
	parsed, err := url.Parse(req.URL)
	if err != nil {
		return template.Template{}, fmt.Errorf("entry %d: bad url: %w", idx, err)
	}

	// Base URL (no query) lands in template.Template.URL; queryString params go
	// into Query so they're visible as independent parameters.
	base := *parsed
	base.RawQuery = ""
	urlBase := base.String()

	queryTpl := map[string]string{}
	for _, q := range req.QueryString {
		if q.Name == "" {
			continue
		}
		queryTpl[q.Name] = q.Value
	}
	// Fallback: if queryString[] is missing (some HAR exporters omit it),
	// lift from the URL ourselves.
	if len(queryTpl) == 0 && parsed.RawQuery != "" {
		for k, vs := range parsed.Query() {
			if len(vs) > 0 {
				queryTpl[k] = vs[0]
			}
		}
	}

	headers := map[string]string{}
	for _, h := range req.Headers {
		if h.Name == "" {
			continue
		}
		if stripHeaders[strings.ToLower(h.Name)] {
			continue
		}
		headers[h.Name] = h.Value
	}

	bodyFormat := ""
	var bodyTemplate json.RawMessage
	if req.PostData != nil {
		bodyFormat, bodyTemplate = harPostDataToTemplate(*req.PostData)
	}

	// Name: <prefix>.<method>_<path-slug>. Duplicates in a single HAR
	// (5 hits to the same endpoint) are disambiguated via a shared
	// counter so each becomes a distinct template.
	base2 := opts.Prefix + "." + sanitiseIdentifier(strings.ToLower(req.Method)+"_"+parsed.Path)
	name := uniquifyName(base2, counters)

	return template.Template{
		Name:         name,
		Method:       strings.ToUpper(req.Method),
		URL:          urlBase,
		Query:        queryTpl,
		Headers:      headers,
		Profile:      opts.Profile,
		BodyFormat:   bodyFormat,
		BodyTemplate: bodyTemplate,
	}, nil
}

// harPostDataToTemplate chooses a body_format based on the captured
// Content-Type. HAR has two quirks vs. the other importers:
//
//  1. params[] is sometimes populated instead of text for form bodies
//     (authoritative when present).
//  2. multipart bodies can't be represented in our body_format types;
//     we skip the body entirely and let the caller keep method/url/
//     headers intact.
//
// Everything else defers to the shared sniffBody.
func harPostDataToTemplate(p harPostData) (string, json.RawMessage) {
	ct := strings.ToLower(p.MimeType)
	// Multipart: drop the body, keep the template.
	if strings.HasPrefix(ct, "multipart/") {
		return "", nil
	}
	// Form bodies with a params[] carry authoritative per-field data
	// that may not reconstruct cleanly from text (quote escaping, for
	// instance). Build the body from params[] directly.
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") && len(p.Params) > 0 {
		obj := map[string]string{}
		for _, kv := range p.Params {
			if kv.Name != "" {
				obj[kv.Name] = kv.Value
			}
		}
		raw, _ := json.Marshal(obj)
		return "form", raw
	}
	return sniffBody(p.Text, p.MimeType)
}

// dedupeKey reduces a template to its identifying shape for --dedupe.
// Body presence (not content) is enough — a HAR might capture 30 hits
// to the same endpoint with different body values, and we want one
// template not 30.
func dedupeKey(t template.Template) string {
	hasBody := "no"
	if len(t.BodyTemplate) > 0 {
		hasBody = "yes"
	}
	return t.Method + " " + t.URL + " " + t.BodyFormat + " " + hasBody
}
