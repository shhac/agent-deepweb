package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"fmt"
	"strings"
)

// ImportCurlOptions for the single-request curl importer.
type ImportCurlOptions struct {
	Name    string // required; final template name
	Profile string
}

// ImportCurl parses one shell-pasted curl invocation into a single
// template.Template and stores it. Handles the flags we see in real-world
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

	st := curlState{headers: map[string]string{}}

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		// Normalise `--flag=value` into two tokens so the dispatch
		// table doesn't need to special-case the `=` form.
		if strings.HasPrefix(t, "--") && strings.Contains(t, "=") {
			eq := strings.IndexByte(t, '=')
			tokens = append(tokens[:i+1:i+1], append([]string{t[eq+1:]}, tokens[i+1:]...)...)
			tokens[i] = t[:eq]
			t = tokens[i]
		}

		flag, known := curlFlags[t]
		if !known {
			// Unknown flag: skip silently. The URL-positional case is
			// everything that's NOT flag-prefixed.
			if strings.HasPrefix(t, "-") {
				continue
			}
			if st.url == "" {
				st.url = t
			}
			continue
		}
		if flag.takesValue {
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("%s needs a value", t)
			}
			if flag.apply != nil {
				flag.apply(&st, tokens[i+1])
			}
			i++
			continue
		}
		if flag.apply != nil {
			flag.apply(&st, "")
		}
	}

	tpl := template.Template{
		Name:    opts.Name,
		Method:  st.method,
		Profile: opts.Profile,
		Headers: st.headers,
	}
	urlStr := st.url
	bodyParts := st.bodyParts
	contentType := st.contentType

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
		tpl.BodyFormat, tpl.BodyTemplate = sniffBody(body, contentType)
	}

	// Every {{placeholder}} referenced becomes a required string param —
	// curl has no notion of variable defaults, so nil defaults is the
	// right shape here.
	refs := collectTemplateRefs(tpl.URL, queryTpl, tpl.Headers, tpl.BodyTemplate)
	tpl.Parameters = paramsFromRefs(refs, nil)

	if err := template.Store(tpl); err != nil {
		return "", err
	}
	return tpl.Name, nil
}

// curlState accumulates the flag-parse results for one ImportCurl
// call. Each handler in curlFlags mutates it in place; the main
// loop doesn't need to thread the mutable state through its own
// locals.
type curlState struct {
	method      string
	url         string
	headers     map[string]string
	bodyParts   []string
	contentType string
}

// curlFlag describes one curl flag we recognise: whether it takes an
// argument, and how it applies that argument (nil = silently
// consume). Unknown flags fall through to the URL-positional case.
type curlFlag struct {
	takesValue bool
	apply      func(*curlState, string)
}

// curlFlags is the dispatch table. Adding a new flag is one entry.
var curlFlags = map[string]curlFlag{
	// Request shape
	"-X":        {takesValue: true, apply: curlSetMethod},
	"--request": {takesValue: true, apply: curlSetMethod},
	"--url":     {takesValue: true, apply: curlSetURL},
	"-H":        {takesValue: true, apply: curlAddHeader},
	"--header":  {takesValue: true, apply: curlAddHeader},
	// Body
	"-d":               {takesValue: true, apply: curlAddBody},
	"--data":           {takesValue: true, apply: curlAddBody},
	"--data-raw":       {takesValue: true, apply: curlAddBody},
	"--data-binary":    {takesValue: true, apply: curlAddBody},
	"--data-urlencode": {takesValue: true, apply: curlAddBody},
	"--json":           {takesValue: true, apply: curlAddJSONBody},
	// Consumed but not stored — the template shouldn't carry creds.
	// User should bind a --profile of the appropriate type.
	"-u":     {takesValue: true, apply: nil},
	"--user": {takesValue: true, apply: nil},
	"-F":     {takesValue: true, apply: nil}, // multipart: unrepresentable
	"--form": {takesValue: true, apply: nil},
	// Value-taking flags whose values we don't need to preserve.
	"--max-time":        {takesValue: true, apply: nil},
	"--connect-timeout": {takesValue: true, apply: nil},
	"-o":                {takesValue: true, apply: nil},
	"--output":          {takesValue: true, apply: nil},
	"-A":                {takesValue: true, apply: nil},
	"--user-agent":      {takesValue: true, apply: nil},
	"--referer":         {takesValue: true, apply: nil},
	"-e":                {takesValue: true, apply: nil},
	"-b":                {takesValue: true, apply: nil},
	"--cookie":          {takesValue: true, apply: nil},
	"-c":                {takesValue: true, apply: nil},
	"--cookie-jar":      {takesValue: true, apply: nil},
	"--resolve":         {takesValue: true, apply: nil},
	"--cacert":          {takesValue: true, apply: nil},
	"--cert":            {takesValue: true, apply: nil},
	"--key":             {takesValue: true, apply: nil},
	// No-value flags we silently drop (noise from browser Copy-as-cURL).
	"-L":          {takesValue: false, apply: nil},
	"--location":  {takesValue: false, apply: nil},
	"-v":          {takesValue: false, apply: nil},
	"--verbose":   {takesValue: false, apply: nil},
	"-s":          {takesValue: false, apply: nil},
	"--silent":    {takesValue: false, apply: nil},
	"--compressed": {takesValue: false, apply: nil},
	"--http2":     {takesValue: false, apply: nil},
	"--http1.1":   {takesValue: false, apply: nil},
	"--http1.0":   {takesValue: false, apply: nil},
	"-k":          {takesValue: false, apply: nil},
	"--insecure":  {takesValue: false, apply: nil},
}

func curlSetMethod(st *curlState, v string) { st.method = strings.ToUpper(v) }
func curlSetURL(st *curlState, v string)    { st.url = v }

func curlAddHeader(st *curlState, v string) {
	k, val, ok := splitHeader(v)
	if !ok {
		return
	}
	if strings.EqualFold(k, "Content-Type") {
		st.contentType = val
	}
	st.headers[k] = val
}

func curlAddBody(st *curlState, v string) {
	st.bodyParts = append(st.bodyParts, v)
}

// curlAddJSONBody handles `--json <value>` — curl 7.82+'s shortcut
// that implies application/json AND body.
func curlAddJSONBody(st *curlState, v string) {
	st.bodyParts = append(st.bodyParts, v)
	st.contentType = "application/json"
	st.headers["Content-Type"] = "application/json"
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
