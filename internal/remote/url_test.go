package remote

import "testing"

func TestResolveURLSuppliesTheMissingScheme(t *testing.T) {
	cases := map[string]string{
		// The complaint this exists for: Chrome refuses a bare host outright.
		"example.com":          "https://example.com",
		"www.iana.org/domains": "https://www.iana.org/domains",
		"  example.com  ":      "https://example.com",
		"example.com?a=b":      "https://example.com?a=b",

		// A host with a port reads like a scheme and is not one.
		"localhost:9333":     "https://localhost:9333",
		"127.0.0.1:7788/#/x": "https://127.0.0.1:7788/#/x",

		// Already absolute: left exactly as it came.
		"https://example.com": "https://example.com",
		"http://example.com":  "http://example.com",
		"HTTP://example.com":  "HTTP://example.com",
		"chrome://version":    "chrome://version",

		// Schemes that carry no authority must not be treated as hosts.
		"about:blank":             "about:blank",
		"data:text/html,<p>hi":    "data:text/html,<p>hi",
		"view-source:https://a.b": "view-source:https://a.b",

		// Protocol-relative, as an href copied out of a page can be.
		"//example.com/a": "https://example.com/a",

		// Nothing in, nothing invented; the caller reports it.
		"":    "",
		"   ": "",
	}
	for in, want := range cases {
		if got := resolveURL(in); got != want {
			t.Errorf("resolveURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// https, never http: an address typed without a scheme must not silently pick
// the insecure one. A site that only serves http will redirect.
func TestResolveURLPrefersHTTPS(t *testing.T) {
	if got := resolveURL("example.com"); got != "https://example.com" {
		t.Errorf("a bare host became %q, want https", got)
	}
}
