package template

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubstituteString_URLEscape(t *testing.T) {
	s, err := SubstituteString("/users/{{username}}/repo", map[string]any{"username": "alice smith"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "alice%20smith") {
		t.Errorf("expected URL-escaping, got %q", s)
	}
}

func TestSubstituteString_MissingPlaceholder(t *testing.T) {
	_, err := SubstituteString("/x/{{absent}}", map[string]any{}, false)
	if err == nil {
		t.Fatal("expected error for missing placeholder")
	}
}

func TestSubstituteBody_TypePreserving(t *testing.T) {
	tpl := json.RawMessage(`{"title":"{{title}}","priority":"{{priority}}","labels":"{{labels}}","nested":{"flag":"{{flag}}"}}`)
	params := map[string]any{
		"title":    "hello",
		"priority": int64(5),
		"labels":   []string{"a", "b"},
		"flag":     true,
	}
	out, err := SubstituteBody(tpl, params)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["title"] != "hello" {
		t.Errorf("title: got %v", decoded["title"])
	}
	// JSON decode coerces int64 → float64, so 5.0.
	if p, ok := decoded["priority"].(float64); !ok || p != 5 {
		t.Errorf("priority type-preservation broken: %v (%T)", decoded["priority"], decoded["priority"])
	}
	labels, ok := decoded["labels"].([]any)
	if !ok || len(labels) != 2 || labels[0] != "a" {
		t.Errorf("labels type-preservation broken: %v", decoded["labels"])
	}
	nested := decoded["nested"].(map[string]any)
	if f, ok := nested["flag"].(bool); !ok || f != true {
		t.Errorf("nested bool: %v (%T)", nested["flag"], nested["flag"])
	}
}

func TestExpandURL_PathAndQuery(t *testing.T) {
	url, err := ExpandURL(
		"https://example.com/users/{{id}}",
		map[string]string{"page": "{{p}}", "sort": "asc"},
		map[string]any{"id": "42", "p": "3"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "https://example.com/users/42?") {
		t.Errorf("URL expansion: %s", url)
	}
	if !strings.Contains(url, "page=3") || !strings.Contains(url, "sort=asc") {
		t.Errorf("URL expansion missing query: %s", url)
	}
}

func TestLint_UnusedAndUnreferencedParams(t *testing.T) {
	tpl := &Template{
		URL:    "https://example.com/{{known}}",
		Method: "GET",
		Parameters: map[string]ParamSpec{
			"known":   {Type: "string"},
			"dangler": {Type: "string"},
		},
	}
	issues := tpl.Lint()
	joined := strings.Join(issues, "\n")
	if !strings.Contains(joined, "dangler") {
		t.Errorf("expected lint to flag unused param 'dangler', got %v", issues)
	}
	if strings.Contains(joined, "known") {
		t.Errorf("lint should not flag used param 'known', got %v", issues)
	}
}

func TestLint_PlaceholderWithoutSpec(t *testing.T) {
	tpl := &Template{
		URL:    "https://example.com/{{unknown}}",
		Method: "GET",
	}
	issues := tpl.Lint()
	if len(issues) == 0 || !strings.Contains(strings.Join(issues, "\n"), "unknown") {
		t.Errorf("expected lint to flag undeclared placeholder 'unknown', got %v", issues)
	}
}
