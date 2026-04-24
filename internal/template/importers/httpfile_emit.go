package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Request-line regex: `METHOD URL[ HTTP/<version>]`. Methods are
// restricted to the common HTTP verbs so `{` on its own line isn't
// mistaken for a GET with a URL of "{".
var requestLineRe = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(\S+)(\s+HTTP/[\d.]+)?\s*$`)

// headerRe matches `Header-Name: value`. Used after the request line
// until we hit a blank line.
var headerRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_-]*)\s*:\s*(.*)$`)

// blockToTemplate translates a parsed httpBlock into a template.Template.
// Walks the block's lines as a tiny state machine: request-line →
// headers → (blank) → body. Name comes from the block's `###`
// trailing label when present, else is synthesised from the method
// + URL path; the shared counters map disambiguates duplicates.
func blockToTemplate(b httpBlock, vars map[string]string, opts ImportHTTPFileOptions, counters map[string]int) (template.Template, error) {
	var method, url string
	headers := map[string]string{}
	var bodyLines []string

	inBody := false
	for _, line := range b.Lines {
		if method == "" {
			// Skip blank lines before the request line.
			if strings.TrimSpace(line) == "" {
				continue
			}
			m := requestLineRe.FindStringSubmatch(line)
			if m == nil {
				return template.Template{}, fmt.Errorf("block %q: no request line (expected 'METHOD URL')", b.Name)
			}
			method = m[1]
			url = m[2]
			continue
		}
		if !inBody {
			if strings.TrimSpace(line) == "" {
				inBody = true
				continue
			}
			hm := headerRe.FindStringSubmatch(line)
			if hm == nil {
				// A non-header, non-blank line before we've seen the
				// blank terminator = the user skipped the blank and
				// went straight to body. Treat as body start.
				inBody = true
				bodyLines = append(bodyLines, line)
				continue
			}
			headers[hm[1]] = strings.TrimSpace(hm[2])
			continue
		}
		bodyLines = append(bodyLines, line)
	}

	if method == "" {
		return template.Template{}, fmt.Errorf("block %q: no request line (expected 'METHOD URL')", b.Name)
	}

	// Strip the ?query suffix into Query, same as the other importers,
	// so each parameter is overridable at run time.
	urlBase, queryTpl := splitQuery(url)

	body := strings.Join(bodyLines, "\n")
	body = strings.TrimSpace(body)
	bodyFormat := ""
	var bodyTemplate json.RawMessage
	if body != "" {
		bodyFormat, bodyTemplate = httpBlockBodyToTemplate(body, headers)
	}

	// Name: the block's ### trailing label if present, else
	// <method>_<path-slug>. Duplicate labels disambiguated via the
	// shared counter.
	leaf := b.Name
	if leaf == "" {
		leaf = strings.ToLower(method) + "_" + urlPath(urlBase)
	}
	base := opts.Prefix + "." + sanitiseIdentifier(leaf)
	name := uniquifyName(base, counters)

	// Each {{name}} in URL/query/headers/body becomes a parameter.
	// `@var = value` declarations (accumulated in vars) seed defaults.
	refs := collectTemplateRefs(urlBase, queryTpl, headers, bodyTemplate)
	params := paramsFromRefs(refs, vars)

	return template.Template{
		Name:         name,
		Method:       method,
		URL:          urlBase,
		Query:        queryTpl,
		Headers:      headers,
		Profile:      opts.Profile,
		BodyFormat:   bodyFormat,
		BodyTemplate: bodyTemplate,
		Parameters:   params,
	}, nil
}

// httpBlockBodyToTemplate extracts the Content-Type header from the
// request's headers map and delegates to the shared sniffer. Unlike
// Postman the .http format doesn't self-describe the body language,
// so we rely on CT + shape sniffing.
func httpBlockBodyToTemplate(body string, headers map[string]string) (string, json.RawMessage) {
	ct := ""
	for k, v := range headers {
		if strings.EqualFold(k, "Content-Type") {
			ct = v
			break
		}
	}
	return sniffBody(body, ct)
}

// urlPath extracts just the path component from a URL (or placeholder
// string) for naming. Best-effort — on an invalid URL we fall back
// to sanitising the whole thing.
func urlPath(u string) string {
	// Strip scheme://host if present.
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			return rest[j:]
		}
		return "/"
	}
	return u
}
