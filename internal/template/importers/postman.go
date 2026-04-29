package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ImportPostmanOptions mirrors ImportOpenAPIOptions for the Postman
// flow. --prefix namespaces, --profile binds, --folder filter narrows.
type ImportPostmanOptions struct {
	Prefix     string
	Profile    string
	FolderPath string // match any item ancestor folder name; empty = no filter
}

// ImportPostmanFile reads a Postman Collection v2.1 JSON file and
// stores one template per request. Folder hierarchy is flattened into
// the template name (prefix.folder_subfolder_requestname) so you can
// still see the grouping after import.
func ImportPostmanFile(path string, opts ImportPostmanOptions) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ImportPostman(data, opts)
}

// ImportPostman parses a Postman Collection v2.1 document from data.
func ImportPostman(data []byte, opts ImportPostmanOptions) ([]string, error) {
	if opts.Prefix == "" {
		return nil, fmt.Errorf("postman import requires --prefix (unique name-space for this collection)")
	}

	var doc postmanDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse postman collection: %w", err)
	}

	// Postman schemas are versioned via a URL in info.schema — v2.1 is
	// "https://schema.getpostman.com/json/collection/v2.1.0/collection.json".
	// We match loosely so forks that drop the fragment still work.
	if doc.Info.Schema != "" && !strings.Contains(doc.Info.Schema, "v2.1") && !strings.Contains(doc.Info.Schema, "v2.0") {
		return nil, fmt.Errorf("unsupported postman schema %q (need collection v2.x)", doc.Info.Schema)
	}

	// Collection-level variables seed the template.ParamSpec defaults for every
	// template we emit. Folder-scoped variables override at their level
	// (walkItems threads the effective set through).
	baseVars := variablesToMap(doc.Variable)

	var imported []string
	// Recursive closure: declare the type up-front so the body can
	// reference itself without the two-step no-op dance.
	var walk func(items []postmanItem, ancestors []string, vars map[string]string) error
	walk = func(items []postmanItem, ancestors []string, vars map[string]string) error {
		for _, it := range items {
			if len(it.Item) > 0 {
				// Folder. Merge its variables on top of the inherited set.
				merged := copyMap(vars)
				for k, v := range variablesToMap(it.Variable) {
					merged[k] = v
				}
				if err := walk(it.Item, append(ancestors, it.Name), merged); err != nil {
					return err
				}
				continue
			}
			// Leaf request. Apply --folder filter if set.
			if opts.FolderPath != "" && !anyAncestorMatches(ancestors, opts.FolderPath) {
				continue
			}
			tpl, err := postmanItemToTemplate(it, ancestors, vars, opts)
			if err != nil {
				return fmt.Errorf("item %q: %w", it.Name, err)
			}
			if err := template.Store(tpl); err != nil {
				return err
			}
			imported = append(imported, tpl.Name)
		}
		return nil
	}
	if err := walk(doc.Item, nil, baseVars); err != nil {
		return imported, err
	}
	sort.Strings(imported)
	return imported, nil
}

// postmanDoc covers the v2.1 fields we consume. Undocumented fields
// (event/script, description, protocolProfileBehavior) are ignored;
// there's no safe translation for scripts into agent-deepweb's
// declarative template world.
type postmanDoc struct {
	Info     postmanInfo       `json:"info"`
	Item     []postmanItem     `json:"item"`
	Variable []postmanVariable `json:"variable"`
}

type postmanInfo struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
}

type postmanItem struct {
	Name     string            `json:"name"`
	Item     []postmanItem     `json:"item"`     // non-empty means it's a folder
	Request  *postmanRequest   `json:"request"`  // non-nil means it's a leaf
	Variable []postmanVariable `json:"variable"` // folder-scoped
}

type postmanRequest struct {
	Method string          `json:"method"`
	URL    json.RawMessage `json:"url"` // string OR object; we peek
	Header []postmanHeader `json:"header"`
	Body   *postmanBody    `json:"body"`
}

type postmanHeader struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

type postmanBody struct {
	Mode       string           `json:"mode"` // raw | urlencoded | formdata | file | graphql
	Raw        string           `json:"raw"`
	URLEncoded []postmanBodyKV  `json:"urlencoded"`
	FormData   []postmanBodyKV  `json:"formdata"`
	Options    *postmanBodyOpts `json:"options"`
	GraphQL    *postmanGraphQL  `json:"graphql"`
}

type postmanBodyKV struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
	Type     string `json:"type"` // "text" / "file" — we take "text" only
}

type postmanBodyOpts struct {
	Raw postmanRawOpts `json:"raw"`
}

type postmanRawOpts struct {
	Language string `json:"language"` // "json" / "xml" / "text" / ...
}

type postmanGraphQL struct {
	Query     string `json:"query"`
	Variables string `json:"variables"`
}

type postmanVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

// postmanURL is the expanded URL object when url is not a plain string.
type postmanURL struct {
	Raw      string              `json:"raw"`
	Protocol string              `json:"protocol"`
	Host     []string            `json:"host"`
	Path     []string            `json:"path"`
	Query    []postmanURLQueryKV `json:"query"`
}

type postmanURLQueryKV struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

func postmanItemToTemplate(it postmanItem, ancestors []string, vars map[string]string, opts ImportPostmanOptions) (template.Template, error) {
	req := it.Request
	if req == nil {
		return template.Template{}, fmt.Errorf("leaf item has no request")
	}

	url, queryTpl, err := parsePostmanURL(req.URL)
	if err != nil {
		return template.Template{}, err
	}

	headers := map[string]string{}
	for _, h := range req.Header {
		if h.Disabled || h.Key == "" {
			continue
		}
		headers[h.Key] = h.Value
	}

	bodyFormat := ""
	var bodyTemplate json.RawMessage
	if req.Body != nil {
		bodyFormat, bodyTemplate = postmanBodyToTemplate(req.Body)
	}

	// Every {{var}} referenced across URL/query/headers/body becomes a
	// parameter. Defaults come from the inherited collection + folder
	// variable map; refs without a default are marked required.
	refs := collectTemplateRefs(url, queryTpl, headers, bodyTemplate)
	params := paramsFromRefs(refs, vars)

	segs := append([]string{}, ancestors...)
	if it.Name != "" {
		segs = append(segs, it.Name)
	}
	name := opts.Prefix + "." + sanitiseIdentifier(strings.Join(segs, "_"))

	return template.Template{
		Name:         name,
		Description:  strings.TrimSpace(it.Name),
		Method:       strings.ToUpper(req.Method),
		URL:          url,
		Query:        queryTpl,
		Headers:      headers,
		Profile:      opts.Profile,
		BodyFormat:   bodyFormat,
		BodyTemplate: bodyTemplate,
		Parameters:   params,
	}, nil
}

// parsePostmanURL handles the dual shape: request.url is either a
// string (the full URL, possibly with {{var}} placeholders) or an
// object with raw/host/path/query. When the object has both raw and
// path, raw usually wins — it's the canonical form the Postman UI
// stores.
func parsePostmanURL(raw json.RawMessage) (string, map[string]string, error) {
	if len(raw) == 0 {
		return "", nil, fmt.Errorf("request.url is missing")
	}
	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		// Strip the ?query part and promote each key to queryTpl.
		urlBase, qtpl := splitQuery(s)
		return urlBase, qtpl, nil
	}
	// Try object.
	var o postmanURL
	if err := json.Unmarshal(raw, &o); err != nil {
		return "", nil, fmt.Errorf("request.url is neither string nor object: %w", err)
	}
	qtpl := map[string]string{}
	for _, q := range o.Query {
		if q.Disabled || q.Key == "" {
			continue
		}
		qtpl[q.Key] = q.Value
	}
	if o.Raw != "" {
		base, _ := splitQuery(o.Raw)
		return base, qtpl, nil
	}
	// Reconstruct from parts.
	scheme := o.Protocol
	if scheme == "" {
		scheme = "https"
	}
	host := strings.Join(o.Host, ".")
	path := strings.Join(o.Path, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, path), qtpl, nil
}

// postmanBodyToTemplate maps the Body.Mode enum to our body_format +
// body_template pair.
func postmanBodyToTemplate(b *postmanBody) (string, json.RawMessage) {
	switch b.Mode {
	case "raw":
		lang := ""
		if b.Options != nil {
			lang = b.Options.Raw.Language
		}
		if strings.EqualFold(lang, "json") || looksLikeJSON(b.Raw) {
			// body_format=json requires body_template to be JSON. The
			// raw text IS already JSON, so use it directly.
			return "json", json.RawMessage(b.Raw)
		}
		// raw format wants body_template as a JSON string literal.
		s, _ := json.Marshal(b.Raw)
		return "raw", s
	case "urlencoded":
		obj := map[string]string{}
		for _, kv := range b.URLEncoded {
			if kv.Disabled || kv.Type == "file" {
				continue
			}
			obj[kv.Key] = kv.Value
		}
		raw, _ := json.Marshal(obj)
		return "form", raw
	case "formdata":
		// Multipart with mixed text+file parts — our body_format doesn't
		// handle file parts yet. Fall back to form-encoded for text fields
		// only and drop files; users can `--file` at run-time (template
		// run doesn't currently support --file, so this is lossy — document).
		obj := map[string]string{}
		for _, kv := range b.FormData {
			if kv.Disabled || kv.Type == "file" {
				continue
			}
			obj[kv.Key] = kv.Value
		}
		raw, _ := json.Marshal(obj)
		return "form", raw
	case "graphql":
		if b.GraphQL == nil {
			return "", nil
		}
		gq := map[string]any{"query": b.GraphQL.Query}
		if b.GraphQL.Variables != "" {
			var v any
			if err := json.Unmarshal([]byte(b.GraphQL.Variables), &v); err == nil {
				gq["variables"] = v
			}
		}
		raw, _ := json.Marshal(gq)
		return "json", raw
	}
	return "", nil
}

func variablesToMap(vs []postmanVariable) map[string]string {
	out := map[string]string{}
	for _, v := range vs {
		if v.Key != "" {
			out[v.Key] = v.Value
		}
	}
	return out
}
