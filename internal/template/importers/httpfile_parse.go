package importers

import (
	"regexp"
	"strings"
)

// httpBlock is a single request block from a .http file — the lines
// between two `###` separators, with the optional trailing name on
// the leading `###` captured separately.
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

// hasNonBlank reports whether any line in lines contains non-whitespace.
// Used to filter out blocks that were just variable declarations.
func hasNonBlank(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return true
		}
	}
	return false
}
