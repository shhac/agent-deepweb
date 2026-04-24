package template

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ImportHTTPFileOptions controls .http file translation. --prefix
// namespaces, --profile binds. No --folder / --tag filters — .http
// files are typically small enough to import wholesale.
type ImportHTTPFileOptions struct {
	Prefix  string
	Profile string
}

// ImportHTTPFile reads a VS Code REST Client / JetBrains HTTP Client
// `.http` file and stores one Template per request block.
//
// Grammar (subset):
//
//	@varName = value           variable declaration (applies to all
//	                           subsequent requests)
//	###                        request separator (optional trailing name)
//	GET https://... HTTP/1.1   request line (HTTP version optional)
//	Header: value              one header per line until blank line
//	                           (blank line separates headers from body)
//	<body lines until next ###, EOF, or another ### separator>
//	# comment                  line comment
//
// Placeholders `{{name}}` pass through unchanged.
func ImportHTTPFile(path string, opts ImportHTTPFileOptions) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ImportHTTPText(string(data), opts)
}

// ImportHTTPText is the pure parser entrypoint — accepts the file
// contents as a string so tests can drive it without a tempfile.
func ImportHTTPText(text string, opts ImportHTTPFileOptions) ([]string, error) {
	if opts.Prefix == "" {
		return nil, fmt.Errorf("http-file import requires --prefix")
	}

	// Split into per-request blocks on the `###` separator. An optional
	// trailing name after the ### becomes the template's local name.
	blocks := splitHTTPBlocks(text)
	if len(blocks) == 0 {
		return nil, fmt.Errorf(".http file has no request blocks")
	}

	vars := map[string]string{}
	var imported []string
	counters := map[string]int{} // for auto-naming duplicates
	for _, b := range blocks {
		// Extract + strip variable declarations BEFORE treating the
		// block as a request. Variables set here apply to all subsequent
		// blocks (REST Client convention).
		b.Lines, vars = extractVars(b.Lines, vars)
		if !hasNonBlank(b.Lines) {
			// A header block that was only @var declarations (or blank
			// lines) is metadata for later requests — skip silently.
			continue
		}
		tpl, err := blockToTemplate(b, vars, opts, counters)
		if err != nil {
			return imported, err
		}
		if err := Store(tpl); err != nil {
			return imported, err
		}
		imported = append(imported, tpl.Name)
	}
	return imported, nil
}

type httpBlock struct {
	Name  string // empty when the ### line had no trailing name
	Lines []string
}

var sepRe = regexp.MustCompile(`^###\s*(.*)$`)

// splitHTTPBlocks walks the text line by line, accumulating lines
// into the current block until a `###` separator. Comments (`# ...`,
// `// ...`) are stripped at parse time to keep the block content
// focused on request data.
func splitHTTPBlocks(text string) []httpBlock {
	// Normalise CRLF → LF so a Windows-exported file lines up.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")

	var blocks []httpBlock
	current := httpBlock{}
	flush := func() {
		// Drop a block that's entirely whitespace / vars — blockToTemplate
		// will reject empty ones, but we can skip here to keep the
		// output clean.
		for _, l := range current.Lines {
			if strings.TrimSpace(l) != "" {
				blocks = append(blocks, current)
				return
			}
		}
	}
	for _, line := range lines {
		if m := sepRe.FindStringSubmatch(line); m != nil {
			flush()
			current = httpBlock{Name: strings.TrimSpace(m[1])}
			continue
		}
		// Line comments: strip before accumulating. `# @foo = bar` is
		// NOT a comment — REST Client uses `@var = value` even without
		// a separator — but a bare `# foo` is.
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "###") {
			continue
		}
		current.Lines = append(current.Lines, line)
	}
	flush()
	return blocks
}

var varRe = regexp.MustCompile(`^\s*@([A-Za-z0-9_-]+)\s*=\s*(.*)$`)

// extractVars pulls `@name = value` lines out of lines into vars and
// returns the remainder. Vars persist across blocks (the caller
// threads the accumulator).
func extractVars(lines []string, vars map[string]string) ([]string, map[string]string) {
	out := vars
	if out == nil {
		out = map[string]string{}
	}
	var remaining []string
	for _, l := range lines {
		if m := varRe.FindStringSubmatch(l); m != nil {
			out[m[1]] = strings.TrimSpace(m[2])
			continue
		}
		remaining = append(remaining, l)
	}
	return remaining, out
}

// Request-line regex: `METHOD URL[ HTTP/<version>]`. Methods are
// restricted to the common HTTP verbs so `{` on its own line isn't
// mistaken for a GET with a URL of "{".
var requestLineRe = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(\S+)(\s+HTTP/[\d.]+)?\s*$`)

// headerRe matches `Header-Name: value`. Used after the request line
// until we hit a blank line.
var headerRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_-]*)\s*:\s*(.*)$`)

func hasNonBlank(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return true
		}
	}
	return false
}

func blockToTemplate(b httpBlock, vars map[string]string, opts ImportHTTPFileOptions, counters map[string]int) (Template, error) {
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
				return Template{}, fmt.Errorf("block %q: no request line (expected 'METHOD URL')", b.Name)
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
		return Template{}, fmt.Errorf("block %q: no request line (expected 'METHOD URL')", b.Name)
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

	// Name: block's ### trailing label if any, else <method>_<path-slug>
	// with a counter for duplicates.
	leaf := b.Name
	if leaf == "" {
		leaf = strings.ToLower(method) + "_" + urlPath(urlBase)
	}
	base := opts.Prefix + "." + sanitiseIdentifier(leaf)
	counters[base]++
	name := base
	if counters[base] > 1 {
		name = fmt.Sprintf("%s_%d", base, counters[base])
	}

	// Variable defaults — any {{name}} referenced by URL/headers/body
	// with a seen @var declaration gets its default.
	refs := map[string]bool{}
	collectPlaceholdersInto(urlBase, refs)
	for k, v := range queryTpl {
		collectPlaceholdersInto(k, refs)
		collectPlaceholdersInto(v, refs)
	}
	for k, v := range headers {
		collectPlaceholdersInto(k, refs)
		collectPlaceholdersInto(v, refs)
	}
	if len(bodyTemplate) > 0 {
		collectPlaceholdersInto(string(bodyTemplate), refs)
	}
	params := map[string]ParamSpec{}
	for ref := range refs {
		spec := ParamSpec{Type: "string"}
		if def, ok := vars[ref]; ok {
			spec.Default = def
		} else {
			spec.Required = true
		}
		params[ref] = spec
	}

	return Template{
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

// httpBlockBodyToTemplate picks a body_format from Content-Type +
// body shape. Unlike Postman the .http format doesn't self-describe
// the body language, so we sniff: JSON → body_format=json; form
// content-type → body_format=form; else → raw.
func httpBlockBodyToTemplate(body string, headers map[string]string) (string, json.RawMessage) {
	ct := ""
	for k, v := range headers {
		if strings.EqualFold(k, "Content-Type") {
			ct = strings.ToLower(v)
			break
		}
	}
	switch {
	case strings.HasPrefix(ct, "application/json"), looksLikeJSON(body):
		var any interface{}
		if err := json.Unmarshal([]byte(body), &any); err == nil {
			return "json", json.RawMessage(body)
		}
		// Fall through to raw if the JSON doesn't parse.
		s, _ := json.Marshal(body)
		return "raw", s
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		// Parse body as query-string → JSON object for body_template.
		obj := map[string]string{}
		for _, pair := range strings.Split(body, "&") {
			if pair == "" {
				continue
			}
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) == 2 {
				obj[kv[0]] = kv[1]
			}
		}
		raw, _ := json.Marshal(obj)
		return "form", raw
	}
	s, _ := json.Marshal(body)
	return "raw", s
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
