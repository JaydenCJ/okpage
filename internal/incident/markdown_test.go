// Tests for the markdown subset renderer, with emphasis on the security
// properties: everything escaped, dangerous link schemes neutralized.
package incident

import (
	"strings"
	"testing"
)

func TestRenderParagraphs(t *testing.T) {
	got := RenderMarkdown("First paragraph\nstill first.\n\nSecond paragraph.")
	want := "<p>First paragraph still first.</p>\n<p>Second paragraph.</p>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderHeadings(t *testing.T) {
	// Headings shift down two levels (# → h3) to nest inside the page's
	// own h1/h2 outline, clamping at h6.
	got := RenderMarkdown("# Top\n\n## Sub\n\n###### Deep")
	for _, want := range []string{"<h3>Top</h3>", "<h4>Sub</h4>", "<h6>Deep</h6>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}

	// A # without a following space is text, not a heading.
	got = RenderMarkdown("#hashtag is just text")
	if strings.Contains(got, "<h") || !strings.Contains(got, "#hashtag") {
		t.Fatalf("hashtag mishandled: %q", got)
	}
}

func TestRenderLists(t *testing.T) {
	got := RenderMarkdown("- first\n- second\n* third")
	want := "<ul>\n<li>first</li>\n<li>second</li>\n<li>third</li>\n</ul>"
	if got != want {
		t.Fatalf("unordered: got %q", got)
	}

	got = RenderMarkdown("1. one\n2. two\n10. ten")
	want = "<ol>\n<li>one</li>\n<li>two</li>\n<li>ten</li>\n</ol>"
	if got != want {
		t.Fatalf("ordered: got %q", got)
	}
}

func TestRenderBlockquote(t *testing.T) {
	got := RenderMarkdown("> quoted line\n> continues")
	want := "<blockquote><p>quoted line continues</p></blockquote>"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestRenderCodeFences(t *testing.T) {
	got := RenderMarkdown("```\ncurl -s http://127.0.0.1/health\n# not a heading\n**not bold**\n```")
	if !strings.Contains(got, "<pre><code>") {
		t.Fatalf("no code block: %q", got)
	}
	if !strings.Contains(got, "# not a heading") || !strings.Contains(got, "**not bold**") {
		t.Fatalf("fence content must stay literal: %q", got)
	}
	if strings.Contains(got, "<strong>") || strings.Contains(got, "<h3>") {
		t.Fatalf("markdown leaked into fence: %q", got)
	}

	// An unterminated fence must render its content, not swallow the file.
	got = RenderMarkdown("```\ntrailing code with no close")
	if !strings.Contains(got, "trailing code with no close") {
		t.Fatalf("content dropped: %q", got)
	}
}

func TestRenderInlineSpans(t *testing.T) {
	got := RenderMarkdown("mix of **bold**, *italic*, and `code` spans")
	for _, want := range []string{"<strong>bold</strong>", "<em>italic</em>", "<code>code</code>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}

	// Code spans are opaque: asterisks inside stay literal.
	got = RenderMarkdown("run `ls *.go` twice")
	if !strings.Contains(got, "<code>ls *.go</code>") {
		t.Fatalf("asterisk inside code span mangled: %q", got)
	}
}

func TestRenderEmphasisEdgeCases(t *testing.T) {
	got := RenderMarkdown("**bold with *nested* italics**")
	if !strings.Contains(got, "<strong>bold with <em>nested</em> italics</strong>") {
		t.Fatalf("nesting broken: %q", got)
	}

	// Unmatched markers stay literal and never open a span.
	got = RenderMarkdown("a single * star and a lone ` backtick")
	if !strings.Contains(got, "*") || !strings.Contains(got, "`") {
		t.Fatalf("literal markers dropped: %q", got)
	}
	if strings.Contains(got, "<em>") || strings.Contains(got, "<code>") {
		t.Fatalf("unmatched markers opened spans: %q", got)
	}
}

func TestRenderLinks(t *testing.T) {
	got := RenderMarkdown("see [the docs](https://example.test/docs) for details")
	if !strings.Contains(got, `<a href="https://example.test/docs">the docs</a>`) {
		t.Fatalf("got %q", got)
	}

	got = RenderMarkdown("[status](status.json) or [mail](mailto:ops@example.test)")
	if !strings.Contains(got, `<a href="status.json">status</a>`) {
		t.Errorf("relative link rejected: %q", got)
	}
	if !strings.Contains(got, `<a href="mailto:ops@example.test">mail</a>`) {
		t.Errorf("mailto rejected: %q", got)
	}
}

func TestRenderBlocksDangerousLinkSchemes(t *testing.T) {
	for _, src := range []string{
		"[click me](javascript:alert(1))",
		"[x](data:text/html;base64,AAAA)",
		"[x](vbscript:msgbox)",
	} {
		if got := RenderMarkdown(src); strings.Contains(got, "<a ") {
			t.Errorf("dangerous URL became a link: %q -> %q", src, got)
		}
	}
}

func TestRenderEscapesRawHTMLEverywhere(t *testing.T) {
	got := RenderMarkdown(`<script>alert("xss")</script> and <img src=x onerror=y>`)
	if strings.Contains(got, "<script>") || strings.Contains(got, "<img") {
		t.Fatalf("raw HTML must be escaped: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("expected escaped entities: %q", got)
	}

	// Inside block elements too.
	got = RenderMarkdown("# <b>title</b>\n\n- <i>item</i>")
	if strings.Contains(got, "<b>") || strings.Contains(got, "<i>") {
		t.Fatalf("HTML leaked through block elements: %q", got)
	}

	// And inside link hrefs, where a quote could break out of the attribute.
	got = RenderMarkdown(`[x](https://example.test/?q="><script>)`)
	if strings.Contains(got, "<script>") {
		t.Fatalf("href escaping failed: %q", got)
	}
}

func TestRenderIsDeterministicIncludingEmptyInput(t *testing.T) {
	if got := RenderMarkdown(""); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	src := "# H\n\ntext **b** *i* `c` [l](https://example.test)\n\n- a\n- b\n\n```\ncode\n```"
	first := RenderMarkdown(src)
	for i := 0; i < 5; i++ {
		if RenderMarkdown(src) != first {
			t.Fatal("output changed between runs")
		}
	}
}
