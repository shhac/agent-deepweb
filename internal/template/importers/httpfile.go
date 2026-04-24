// httpfile.go — public entrypoints for .http file import (VS Code
// REST Client / JetBrains HTTP Client format). The parser lives in
// httpfile_parse.go (text → httpBlock + vars); the translator lives
// in httpfile_emit.go (httpBlock → template.Template).
package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"fmt"
	"os"
)

// ImportHTTPFileOptions controls .http file translation. --prefix
// namespaces, --profile binds. No --folder / --tag filters — .http
// files are typically small enough to import wholesale.
type ImportHTTPFileOptions struct {
	Prefix  string
	Profile string
}

// ImportHTTPFile reads a VS Code REST Client / JetBrains HTTP Client
// `.http` file and stores one template.Template per request block.
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
		if err := template.Store(tpl); err != nil {
			return imported, err
		}
		imported = append(imported, tpl.Name)
	}
	return imported, nil
}
