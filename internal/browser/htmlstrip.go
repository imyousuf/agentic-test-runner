package browser

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// StripMarkup removes script/style tags and their content, style attributes,
// and on* event handler attributes from HTML, preserving structural markup.
func StripMarkup(raw string) (string, error) {
	tokenizer := html.NewTokenizer(strings.NewReader(raw))

	var buf strings.Builder
	var skipDepth int
	var skipTag atom.Atom

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			err := tokenizer.Err()
			if err.Error() == "EOF" {
				return buf.String(), nil
			}
			return buf.String(), err
		}

		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			a := atom.Lookup(tn)

			if a == atom.Script || a == atom.Style {
				if tt == html.StartTagToken {
					skipDepth = 1
					skipTag = a
				}
				continue
			}

			buf.WriteByte('<')
			buf.Write(tn)

			if hasAttr {
				for {
					key, val, more := tokenizer.TagAttr()
					k := string(key)
					if k == "style" || strings.HasPrefix(k, "on") {
						if !more {
							break
						}
						continue
					}
					buf.WriteByte(' ')
					buf.WriteString(k)
					buf.WriteString(`="`)
					buf.WriteString(html.EscapeString(string(val)))
					buf.WriteByte('"')
					if !more {
						break
					}
				}
			}

			if tt == html.SelfClosingTagToken {
				buf.WriteString(" /")
			}
			buf.WriteByte('>')

		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			a := atom.Lookup(tn)

			if skipDepth > 0 && a == skipTag {
				skipDepth--
				continue
			}
			if skipDepth > 0 {
				continue
			}

			buf.WriteString("</")
			buf.Write(tn)
			buf.WriteByte('>')

		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			buf.Write(tokenizer.Text())

		case html.CommentToken, html.DoctypeToken:
			if skipDepth > 0 {
				continue
			}
			buf.Write(tokenizer.Raw())
		}
	}
}
