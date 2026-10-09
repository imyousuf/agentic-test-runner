package remote

import "strings"

// bareSchemes are the URL schemes that carry no "//" authority.
//
// They matter because the rule below treats anything without "://" as a bare
// host, and "about:blank" would otherwise become "https://about:blank".
var bareSchemes = map[string]bool{
	"about":       true,
	"blob":        true,
	"chrome":      true,
	"data":        true,
	"devtools":    true,
	"file":        true,
	"javascript":  true,
	"mailto":      true,
	"view-source": true,
}

/*
resolveURL supplies the scheme a person leaves out.

Chrome refuses "example.com" outright -- "Cannot navigate to invalid URL" --
so without this the address box only accepted input that already looked like a
URL, which is not how an address box has behaved in twenty years.

https, not http: an address typed without a scheme should not silently choose
the insecure one. A site that only serves http will redirect.
*/
func resolveURL(raw string) string {
	url := strings.TrimSpace(raw)
	if url == "" {
		return url
	}
	// Already absolute.
	if strings.Contains(url, "://") {
		return url
	}
	// Protocol-relative, as an href copied out of a page can be.
	if strings.HasPrefix(url, "//") {
		return "https:" + url
	}
	// A scheme that takes no authority, such as about:blank. Checked before
	// the host rule, because "localhost:9333" looks exactly like one of these
	// and is a host with a port.
	if scheme, _, found := strings.Cut(url, ":"); found && bareSchemes[strings.ToLower(scheme)] {
		return url
	}
	return "https://" + url
}
