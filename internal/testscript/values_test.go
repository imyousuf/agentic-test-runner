package testscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePropertiesBasics(t *testing.T) {
	got, err := ParseProperties(`
# a comment
! also a comment

base_url=https://shop.example.com
search_term = widget
colon_form: yes
empty=
with_spaces = two words
`)
	if err != nil {
		t.Fatalf("ParseProperties: %v", err)
	}

	want := map[string]string{
		"base_url":    "https://shop.example.com",
		"search_term": "widget",
		"colon_form":  "yes",
		"empty":       "",
		"with_spaces": "two words",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d keys, want %d: %v", len(got), len(want), got)
	}
}

func TestParsePropertiesContinuationAndEscapes(t *testing.T) {
	got, err := ParseProperties(`long=one \
two \
three
tabbed=a\tb
newline=a\nb
literal_equals=a\=b`)
	if err != nil {
		t.Fatalf("ParseProperties: %v", err)
	}

	if got["long"] != "one two three" {
		t.Errorf("continuation = %q", got["long"])
	}
	if got["tabbed"] != "a\tb" {
		t.Errorf("tab escape = %q", got["tabbed"])
	}
	if got["newline"] != "a\nb" {
		t.Errorf("newline escape = %q", got["newline"])
	}
	if got["literal_equals"] != "a=b" {
		t.Errorf("escaped separator = %q", got["literal_equals"])
	}
}

func TestParsePropertiesRejectsGarbage(t *testing.T) {
	if _, err := ParseProperties("this line has no separator\n"); err == nil {
		t.Error("expected an error for a line that is not key=value")
	}
}

// The layering is the whole point: shared defaults, per-machine override,
// environment on top for CI.
func TestValuesLayerInPrecedenceOrder(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "shop.test.txt")

	write(t, ValuesPath(spec), "base_url=http://localhost:3000\nsearch_term=widget\nshared=from-values\n")
	write(t, OverridePath(spec), "base_url=https://staging.example.com\nsearch_term=gadget\n")
	t.Setenv("ATR_VALUE_SEARCH_TERM", "from-env")

	v, err := LoadValues(spec)
	if err != nil {
		t.Fatalf("LoadValues: %v", err)
	}

	check := func(key, want, wantOrigin string) {
		t.Helper()
		got, ok := v.Get(key)
		if !ok {
			t.Fatalf("%s not found", key)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
		if o := v.Origin(key); o != wantOrigin {
			t.Errorf("%s came from %q, want %q", key, o, wantOrigin)
		}
	}

	check("shared", "from-values", "values")                     // only in values
	check("base_url", "https://staging.example.com", "override") // override beats values
	check("search_term", "from-env", "environment")              // env beats everything
}

func TestLoadValuesWithNoFilesIsEmptyNotAnError(t *testing.T) {
	v, err := LoadValues(filepath.Join(t.TempDir(), "none.test.txt"))
	if err != nil {
		t.Fatalf("LoadValues: %v", err)
	}
	if len(v.Keys()) != 0 {
		t.Errorf("expected no keys, got %v", v.Keys())
	}
}

// An error about a missing input has to say what to do about it, or the user
// is left guessing which of three layers to edit.
func TestMissingMessageNamesEveryLayer(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "shop.test.txt")
	write(t, ValuesPath(spec), "known=1\n")

	v, err := LoadValues(spec)
	if err != nil {
		t.Fatal(err)
	}

	msg := v.missingMessage("search_term")
	for _, want := range []string{
		"search_term",
		"shop.test.properties",
		"shop.test.override.properties",
		"ATR_VALUE_SEARCH_TERM",
		"known",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should mention %q, got: %s", want, msg)
		}
	}
}

// The values file is the part a human edits. A recompile must not discard
// the hosts and accounts they set up.
func TestSaveValuesDoesNotClobberExisting(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "shop.test.txt")

	write(t, ValuesPath(spec), "base_url=https://mine.example.com\nsearch_term=mine\n")

	if _, err := SaveValues(spec, "http://localhost:3000", "base_url=http://localhost:3000\nsearch_term=widget\nnew_key=added\n"); err != nil {
		t.Fatalf("SaveValues: %v", err)
	}

	v, err := LoadValues(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get("base_url"); got != "https://mine.example.com" {
		t.Errorf("base_url = %q; an existing value must not be overwritten", got)
	}
	if got, _ := v.Get("search_term"); got != "mine" {
		t.Errorf("search_term = %q; an existing value must not be overwritten", got)
	}
	if got, _ := v.Get("new_key"); got != "added" {
		t.Errorf("new_key = %q; a genuinely new key should be added", got)
	}
}

func TestSaveValuesRecordsBaseURL(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "shop.test.txt")

	path, err := SaveValues(spec, "http://localhost:8745", "search_term=widget\n")
	if err != nil {
		t.Fatalf("SaveValues: %v", err)
	}

	v, err := LoadValues(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get("base_url"); got != "http://localhost:8745" {
		t.Errorf("base_url = %q, want the compile-time base recorded", got)
	}

	body := read(t, path)
	if !strings.Contains(body, "Do not put passwords") {
		t.Error("the generated file should warn against storing credentials")
	}
	if !strings.Contains(body, "override.properties") {
		t.Error("the generated file should say where per-machine overrides go")
	}
}

func TestMergeValuesOnlyAddsNewKeys(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "shop.test.txt")
	write(t, ValuesPath(spec), "existing=keep-me\n")

	added, err := MergeValues(spec, "existing=overwritten\nfresh=new-value\n")
	if err != nil {
		t.Fatalf("MergeValues: %v", err)
	}
	if len(added) != 1 || added[0] != "fresh" {
		t.Errorf("added = %v, want [fresh]", added)
	}

	v, err := LoadValues(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get("existing"); got != "keep-me" {
		t.Errorf("existing = %q, want keep-me", got)
	}
	if got, _ := v.Get("fresh"); got != "new-value" {
		t.Errorf("fresh = %q, want new-value", got)
	}
}

func TestValuesPathsSitBesideTheSpec(t *testing.T) {
	if got := ValuesPath("/t/shop.test.txt"); got != "/t/shop.test.properties" {
		t.Errorf("ValuesPath = %q", got)
	}
	if got := OverridePath("/t/shop.test.txt"); got != "/t/shop.test.override.properties" {
		t.Errorf("OverridePath = %q", got)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A base URL routinely names a document rather than a directory. Joining a
// relative path onto it produces login.html/login.html, which the script then
// reports as a missing element — a 404 disguised as drift.
func TestRelativeURLsResolveLikeABrowser(t *testing.T) {
	tests := []struct {
		base, ref, want string
	}{
		{"http://host/login.html", "login.html", "http://host/login.html"},
		{"http://host/login.html", "signup.html", "http://host/signup.html"},
		{"http://host/login.html", "/admin", "http://host/admin"},
		{"http://host/app/", "page", "http://host/app/page"},
		{"http://host/app", "page", "http://host/page"},
		{"http://host/login.html", "", "http://host/login.html"},
		{"http://host/a.html", "https://other/b", "https://other/b"},
		{"", "page.html", "page.html"},
	}

	for _, tt := range tests {
		r := &runtime{opts: Options{BaseURL: tt.base}}
		if got := r.resolve(tt.ref); got != tt.want {
			t.Errorf("resolve(base=%q, ref=%q) = %q, want %q", tt.base, tt.ref, got, tt.want)
		}
	}
}
