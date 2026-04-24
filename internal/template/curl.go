package template

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ImportCurlOptions for the single-request curl importer.
type ImportCurlOptions struct {
	Name    string // required; final template name
	Profile string
}

// ImportCurl parses one shell-pasted curl invocation into a single
// Template and stores it. Handles the flags we see in real-world
// pasted commands:
//
//	-X / --request               method
//	-H / --header                request header
//	-d / --data / --data-raw     body text (first wins)
//	--data-binary                body text
//	--data-urlencode             urlencoded kv appended to body
//	--json                       JSON body (curl 7.82+)
//	-F / --form                  skipped (multipart not representable)
//	-u / --user                  noted but not stored (user should use --profile)
//	--url                        alternative URL flag
//	URL as positional arg        URL (first non-flag token)
//
// Ignored flags (curl emits these all the time from browser "Copy as
// cURL" but they don't affect the template shape):
//
//	-L / --location              we always follow (fetch's default)
//	-v / --verbose, --silent, -s, --compressed, --http2, --http1.1,
//	--max-time, -k / --insecure (requires the profile to opt into
//	allow-http anyway; not surfaced here)
func ImportCurl(cmd string, opts ImportCurlOptions) (string, error) {
	if opts.Name == "" {
		return "", fmt.Errorf("curl import requires --name")
	}
	tokens, err := shellLex(cmd)
	if err != nil {
		return "", err
	}
	if len(tokens) == 0 {
		return "", fmt.Errorf("empty curl command")
	}
	// Drop the leading `curl` token if present (users often paste it).
	if strings.EqualFold(tokens[0], "curl") {
		tokens = tokens[1:]
	}

	tpl := Template{
		Name:    opts.Name,
		Method:  "", // derived below
		Profile: opts.Profile,
		Headers: map[string]string{},
	}
	var urlStr string
	var bodyParts []string
	contentType := ""

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]

		// `--flag=value` form: split once on `=` and treat as two tokens.
		if strings.HasPrefix(t, "--") && strings.Contains(t, "=") {
			eq := strings.IndexByte(t, '=')
			tokens = append(tokens[:i+1:i+1], append([]string{t[eq+1:]}, tokens[i+1:]...)...)
			tokens[i] = t[:eq]
			t = tokens[i]
		}

		switch t {
		case "-X", "--request":
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("%s needs a value", t)
			}
			tpl.Method = strings.ToUpper(tokens[i+1])
			i++
		case "-H", "--header":
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("%s needs a value", t)
			}
			k, v, ok := splitHeader(tokens[i+1])
			if ok {
				if strings.EqualFold(k, "Content-Type") {
					contentType = v
				}
				tpl.Headers[k] = v
			}
			i++
		case "-d", "--data", "--data-raw", "--data-binary":
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("%s needs a value", t)
			}
			bodyParts = append(bodyParts, tokens[i+1])
			i++
		case "--data-urlencode":
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("%s needs a value", t)
			}
			bodyParts = append(bodyParts, tokens[i+1])
			i++
		case "--json":
			// curl 7.82+: --json implies application/json and body.
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("%s needs a value", t)
			}
			bodyParts = append(bodyParts, tokens[i+1])
			contentType = "application/json"
			tpl.Headers["Content-Type"] = contentType
			i++
		case "--url":
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("%s needs a value", t)
			}
			urlStr = tokens[i+1]
			i++
		case "-u", "--user":
			// Noted but not stored — the template shouldn't ship
			// credentials. The user should bind a --profile of type basic.
			i++
		case "-F", "--form":
			// Multipart not representable in body_format today. Skip and
			// let the user know via a lint comment (future: track this
			// and surface in show).
			i++
		// Flags we silently ignore (see doc comment).
		case "-L", "--location", "-v", "--verbose", "-s", "--silent",
			"--compressed", "--http2", "--http1.1", "--http1.0",
			"-k", "--insecure":
			// no-op
		case "--max-time", "--connect-timeout", "-o", "--output", "-A", "--user-agent",
			"--referer", "-e", "-b", "--cookie", "-c", "--cookie-jar",
			"--resolve", "--cacert", "--cert", "--key":
			// These take an argument we consume and ignore.
			if i+1 < len(tokens) {
				i++
			}
		default:
			if strings.HasPrefix(t, "-") {
				// Unknown flag — best-effort skip. If it takes a value
				// we may swallow the next token too; erring on the side
				// of producing a template rather than failing outright.
				continue
			}
			if urlStr == "" {
				urlStr = t
			}
		}
	}

	if urlStr == "" {
		return "", fmt.Errorf("curl command has no URL")
	}
	// Default method: POST if body provided, else GET (same rule as fetch).
	if tpl.Method == "" {
		if len(bodyParts) > 0 {
			tpl.Method = "POST"
		} else {
			tpl.Method = "GET"
		}
	}

	// Lift query string out of the URL for overridability.
	urlBase, queryTpl := splitQuery(urlStr)
	tpl.URL = urlBase
	if len(queryTpl) > 0 {
		tpl.Query = queryTpl
	}

	body := strings.Join(bodyParts, "&")
	if body != "" {
		tpl.BodyFormat, tpl.BodyTemplate = curlBodyToTemplate(body, contentType)
	}

	// Parameters: every {{placeholder}} referenced gets a string spec.
	// Unlike the Postman path we don't have defaults, so all refs are
	// Required.
	refs := map[string]bool{}
	collectPlaceholdersInto(tpl.URL, refs)
	for k, v := range queryTpl {
		collectPlaceholdersInto(k, refs)
		collectPlaceholdersInto(v, refs)
	}
	for k, v := range tpl.Headers {
		collectPlaceholdersInto(k, refs)
		collectPlaceholdersInto(v, refs)
	}
	if len(tpl.BodyTemplate) > 0 {
		collectPlaceholdersInto(string(tpl.BodyTemplate), refs)
	}
	if len(refs) > 0 {
		tpl.Parameters = map[string]ParamSpec{}
		for r := range refs {
			tpl.Parameters[r] = ParamSpec{Type: "string", Required: true}
		}
	}

	if err := Store(tpl); err != nil {
		return "", err
	}
	return tpl.Name, nil
}

// curlBodyToTemplate picks a body_format from Content-Type or a
// JSON-shaped body. Matches the .http and Postman importers so imports
// feel consistent.
func curlBodyToTemplate(body, contentType string) (string, json.RawMessage) {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "application/json"), looksLikeJSON(body):
		var any interface{}
		if err := json.Unmarshal([]byte(body), &any); err == nil {
			return "json", json.RawMessage(body)
		}
		s, _ := json.Marshal(body)
		return "raw", s
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"), looksLikeForm(body):
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

// looksLikeForm returns true for k=v&k2=v2 shaped bodies — a
// conservative sniff that lets a curl without -H Content-Type still
// land as body_format=form when that's clearly what it is.
func looksLikeForm(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "{}[]\n") {
		return false
	}
	// At least one k=v pair.
	if !strings.Contains(s, "=") {
		return false
	}
	for _, pair := range strings.Split(s, "&") {
		if strings.Count(pair, "=") < 1 {
			return false
		}
	}
	return true
}

// splitHeader parses `K: V` into (K, V, ok). Tolerates leading/trailing
// whitespace around the separator.
func splitHeader(s string) (string, string, bool) {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
}

// shellLex is a tiny tokenizer for command lines. Handles single +
// double quotes and backslash escapes. NOT a full shell parser —
// globbing, variable expansion, command substitution, pipes are
// deliberately absent (we're parsing one pasted curl invocation, not
// running a script).
//
// Why roll our own: importing a real shell-word library would add a
// dependency for ~80 lines of logic.
func shellLex(s string) ([]string, error) {
	var (
		tokens []string
		cur    strings.Builder
		inCur  bool
		// quote state: 0 = none, '"' = double, '\'' = single
		quote byte
	)
	i := 0
	for i < len(s) {
		c := s[i]
		switch quote {
		case 0:
			switch {
			case c == ' ' || c == '\t' || c == '\n':
				if inCur {
					tokens = append(tokens, cur.String())
					cur.Reset()
					inCur = false
				}
			case c == '\\' && i+1 < len(s):
				// Outside quotes, a backslash preserves the next char
				// (curl commands pasted with line continuation often
				// land as `\\\n`).
				if s[i+1] == '\n' {
					i++ // skip the newline entirely
					break
				}
				cur.WriteByte(s[i+1])
				inCur = true
				i++
			case c == '"' || c == '\'':
				quote = c
				inCur = true
			default:
				cur.WriteByte(c)
				inCur = true
			}
		case '"':
			switch {
			case c == '\\' && i+1 < len(s):
				// Inside double quotes, backslash escapes ", \, $, ` and newline.
				next := s[i+1]
				if next == '\n' {
					i++
				} else {
					cur.WriteByte(next)
					i++
				}
			case c == '"':
				quote = 0
			default:
				cur.WriteByte(c)
			}
		case '\'':
			// Inside single quotes, everything is literal until the
			// closing '.
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		}
		i++
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if inCur {
		tokens = append(tokens, cur.String())
	}
	return tokens, nil
}
