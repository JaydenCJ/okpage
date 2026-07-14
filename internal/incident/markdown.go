package incident

// A small, safe markdown renderer for incident bodies.
//
// Supported: ATX headings, paragraphs, unordered (-, *) and ordered (1.)
// lists, > blockquotes, ``` code fences, and the inline spans **bold**,
// *italic*, `code`, and [text](href). Everything is HTML-escaped and link
// targets are restricted to http/https/mailto/relative, so an incident file
// can never inject script into the page. Raw HTML in the source is shown
// as text, not interpreted.
//
// Headings are shifted down two levels (# → <h3>) because incident bodies
// render inside an <article> under the page's own <h1>/<h2> outline.

import (
	"fmt"
	"html"
	"strings"
)

// RenderMarkdown converts an incident body to HTML.
func RenderMarkdown(src string) string {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var b strings.Builder
	var paragraph []string

	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		fmt.Fprintf(&b, "<p>%s</p>\n", renderInline(strings.Join(paragraph, " ")))
		paragraph = nil
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			flush()

		case strings.HasPrefix(trimmed, "```"):
			flush()
			var code []string
			i++
			for ; i < len(lines); i++ {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
					break
				}
				code = append(code, lines[i])
			}
			// An unterminated fence swallows the rest of the file — render
			// what we collected rather than dropping it.
			fmt.Fprintf(&b, "<pre><code>%s</code></pre>\n",
				html.EscapeString(strings.Join(code, "\n")))

		case isHeading(trimmed):
			flush()
			level, text := splitHeading(trimmed)
			// Shift into the page outline; clamp at h6.
			level += 2
			if level > 6 {
				level = 6
			}
			fmt.Fprintf(&b, "<h%d>%s</h%d>\n", level, renderInline(text), level)

		case strings.HasPrefix(trimmed, ">"):
			flush()
			var quote []string
			for ; i < len(lines); i++ {
				t := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(t, ">") {
					i--
					break
				}
				quote = append(quote, strings.TrimSpace(strings.TrimPrefix(t, ">")))
			}
			fmt.Fprintf(&b, "<blockquote><p>%s</p></blockquote>\n",
				renderInline(strings.Join(quote, " ")))

		case isBullet(trimmed):
			flush()
			b.WriteString("<ul>\n")
			for ; i < len(lines); i++ {
				t := strings.TrimSpace(lines[i])
				if !isBullet(t) {
					i--
					break
				}
				fmt.Fprintf(&b, "<li>%s</li>\n", renderInline(t[2:]))
			}
			b.WriteString("</ul>\n")

		case isOrdered(trimmed):
			flush()
			b.WriteString("<ol>\n")
			for ; i < len(lines); i++ {
				t := strings.TrimSpace(lines[i])
				if !isOrdered(t) {
					i--
					break
				}
				fmt.Fprintf(&b, "<li>%s</li>\n", renderInline(strings.TrimSpace(t[strings.Index(t, ".")+1:])))
			}
			b.WriteString("</ol>\n")

		default:
			paragraph = append(paragraph, trimmed)
		}
	}
	flush()
	return strings.TrimSuffix(b.String(), "\n")
}

func isHeading(s string) bool {
	if !strings.HasPrefix(s, "#") {
		return false
	}
	level := 0
	for level < len(s) && s[level] == '#' {
		level++
	}
	return level <= 6 && level < len(s) && s[level] == ' '
}

func splitHeading(s string) (int, string) {
	level := 0
	for level < len(s) && s[level] == '#' {
		level++
	}
	return level, strings.TrimSpace(s[level:])
}

func isBullet(s string) bool {
	return strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ")
}

func isOrdered(s string) bool {
	dot := strings.Index(s, ".")
	if dot < 1 || dot+1 >= len(s) || s[dot+1] != ' ' {
		return false
	}
	for _, r := range s[:dot] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// renderInline handles `code`, **bold**, *italic*, and [text](href) spans.
// Code spans are handled first and their contents are opaque, so literal
// asterisks inside backticks survive.
func renderInline(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			if end := strings.Index(s[i+1:], "`"); end >= 0 {
				fmt.Fprintf(&b, "<code>%s</code>", html.EscapeString(s[i+1:i+1+end]))
				i += end + 2
				continue
			}
			b.WriteString("`")
			i++

		case strings.HasPrefix(s[i:], "**"):
			if end := strings.Index(s[i+2:], "**"); end > 0 {
				fmt.Fprintf(&b, "<strong>%s</strong>", renderInline(s[i+2:i+2+end]))
				i += end + 4
				continue
			}
			b.WriteString("**")
			i += 2

		case s[i] == '*':
			if end := strings.Index(s[i+1:], "*"); end > 0 {
				fmt.Fprintf(&b, "<em>%s</em>", renderInline(s[i+1:i+1+end]))
				i += end + 2
				continue
			}
			b.WriteString("*")
			i++

		case s[i] == '[':
			if text, href, rest, ok := parseLink(s[i:]); ok {
				fmt.Fprintf(&b, `<a href="%s">%s</a>`,
					html.EscapeString(href), renderInline(text))
				i = len(s) - len(rest)
				continue
			}
			b.WriteString("[")
			i++

		default:
			// Escape one character at a time; html.EscapeString on the whole
			// run would double-escape the entities written above.
			b.WriteString(html.EscapeString(s[i : i+1]))
			i++
		}
	}
	return b.String()
}

// parseLink parses a leading [text](href). Dangerous schemes are rejected
// so the link renders as plain text instead of becoming clickable.
func parseLink(s string) (text, href, rest string, ok bool) {
	closeBracket := strings.Index(s, "](")
	if closeBracket < 0 {
		return "", "", "", false
	}
	closeParen := strings.Index(s[closeBracket:], ")")
	if closeParen < 0 {
		return "", "", "", false
	}
	text = s[1:closeBracket]
	href = s[closeBracket+2 : closeBracket+closeParen]
	if !safeHref(href) {
		return "", "", "", false
	}
	return text, href, s[closeBracket+closeParen+1:], true
}

// safeHref allows http/https/mailto and scheme-less relative targets.
func safeHref(href string) bool {
	lower := strings.ToLower(strings.TrimSpace(href))
	switch {
	case strings.HasPrefix(lower, "http://"),
		strings.HasPrefix(lower, "https://"),
		strings.HasPrefix(lower, "mailto:"):
		return true
	}
	// Any other explicit scheme (javascript:, data:, vbscript:, …) is unsafe.
	if i := strings.IndexAny(lower, ":/?#"); i >= 0 && lower[i] == ':' {
		return false
	}
	return true
}
