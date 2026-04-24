package template

import (
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

// ImportHARFile reads a HAR 1.2 JSON file and stores one Template per
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
		if err := Store(tpl); err != nil {
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
	"authorization":       true,
	"cookie":              true,
	"set-cookie":          true,
	"x-csrf-token":        true,
	"x-csrftoken":         true,
	"x-xsrf-token":        true,
	"x-api-key":           true, // profile attaches instead
	":authority":          true, // HTTP/2 pseudo-header
	":method":             true,
	":path":               true,
	":scheme":             true,
	"content-length":      true, // auto-computed by the http client
	"host":                true, // auto-set by the http client
	"accept-encoding":     true, // transport-level; user shouldn't override
	"user-agent":          true, // profile's UA / per-request --user-agent wins
}

func harEntryToTemplate(e harEntry, opts ImportHAROptions, idx int, counters map[string]int) (Template, error) {
	req := e.Request
	if req.URL == "" || req.Method == "" {
		return Template{}, fmt.Errorf("entry %d: missing url or method", idx)
	}
	parsed, err := url.Parse(req.URL)
	if err != nil {
		return Template{}, fmt.Errorf("entry %d: bad url: %w", idx, err)
	}

	// Base URL (no query) lands in Template.URL; queryString params go
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

	// Name: <prefix>.<method>_<path-slug>. Duplicates disambiguated via
	// a per-name counter so a single HAR with 5 requests to /api/items
	// produces 5 distinct templates.
	base2 := opts.Prefix + "." + sanitiseIdentifier(strings.ToLower(req.Method)+"_"+parsed.Path)
	counters[base2]++
	name := base2
	if counters[base2] > 1 {
		name = fmt.Sprintf("%s_%d", base2, counters[base2])
	}

	return Template{
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
// Content-Type. HAR exports the body as text (for most types) or as
// params (for x-www-form-urlencoded). Files are dropped — our body
// templates don't carry binary content anyway.
func harPostDataToTemplate(p harPostData) (string, json.RawMessage) {
	ct := strings.ToLower(p.MimeType)
	switch {
	case strings.HasPrefix(ct, "application/json"):
		// text is already valid JSON; pass through as body_template.
		if strings.TrimSpace(p.Text) == "" {
			return "", nil
		}
		// Validate by unmarshal so we don't store a body_template that
		// fails the template engine's JSON check later.
		var any interface{}
		if err := json.Unmarshal([]byte(p.Text), &any); err != nil {
			// Fall back to raw so the import doesn't fail outright.
			s, _ := json.Marshal(p.Text)
			return "raw", s
		}
		return "json", json.RawMessage(p.Text)
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		obj := map[string]string{}
		if len(p.Params) > 0 {
			for _, kv := range p.Params {
				if kv.Name != "" {
					obj[kv.Name] = kv.Value
				}
			}
		} else if p.Text != "" {
			// Parse Text as query-string if params[] wasn't populated.
			vals, err := url.ParseQuery(p.Text)
			if err == nil {
				for k, vs := range vals {
					if len(vs) > 0 {
						obj[k] = vs[0]
					}
				}
			}
		}
		raw, _ := json.Marshal(obj)
		return "form", raw
	case strings.HasPrefix(ct, "multipart/"):
		// Multipart with file parts can't be round-tripped through our
		// body_format types. Skip the body; the template still captures
		// method/url/headers.
		return "", nil
	default:
		if strings.TrimSpace(p.Text) == "" {
			return "", nil
		}
		s, _ := json.Marshal(p.Text)
		return "raw", s
	}
}

// dedupeKey reduces a template to its identifying shape for --dedupe.
// Body presence (not content) is enough — a HAR might capture 30 hits
// to the same endpoint with different body values, and we want one
// template not 30.
func dedupeKey(t Template) string {
	hasBody := "no"
	if len(t.BodyTemplate) > 0 {
		hasBody = "yes"
	}
	return t.Method + " " + t.URL + " " + t.BodyFormat + " " + hasBody
}
