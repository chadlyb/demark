package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "regenerate testdata pass lists from current results")

// ---------------------------------------------------------------------------
// Unit tests for demark's own behavior (including extensions beyond
// CommonMark: heading ids, tables, strikethrough).

func TestMarkdownBody(t *testing.T) {
	cases := []struct {
		name, md, want string
	}{
		{"h1 with id", "# Hello World", "<h1 id=\"hello-world\">Hello World</h1>\n"},
		{"h6", "###### deep", "<h6 id=\"deep\">deep</h6>\n"},
		{"atx trailing hashes", "## Trimmed ##", "<h2 id=\"trimmed\">Trimmed</h2>\n"},
		{"setext h1", "Title\n=====", "<h1 id=\"title\">Title</h1>\n"},
		{"setext h2", "Title\n-----", "<h2 id=\"title\">Title</h2>\n"},
		{"paragraph", "plain text", "<p>plain text</p>\n"},
		{"emphasis", "*em* **strong** ***both***",
			"<p><em>em</em> <strong>strong</strong> <em><strong>both</strong></em></p>\n"},
		{"underscore emphasis", "_em_ __strong__",
			"<p><em>em</em> <strong>strong</strong></p>\n"},
		{"intraword underscore", "snake_case_word", "<p>snake_case_word</p>\n"},
		{"nested emphasis", "*outer **inner** outer*",
			"<p><em>outer <strong>inner</strong> outer</em></p>\n"},
		{"strikethrough", "~~gone~~", "<p><del>gone</del></p>\n"},
		{"inline code", "use `x := 1` here", "<p>use <code>x := 1</code> here</p>\n"},
		{"code span backtick", "`` ` ``", "<p><code>`</code></p>\n"},
		{"code span no nesting", "`a *b* c`", "<p><code>a *b* c</code></p>\n"},
		{"link", "[text](https://example.com)",
			"<p><a href=\"https://example.com\">text</a></p>\n"},
		{"link with title", "[t](/u \"ti\")", "<p><a href=\"/u\" title=\"ti\">t</a></p>\n"},
		{"image", "![alt](/img.png)", "<p><img src=\"/img.png\" alt=\"alt\"></p>\n"},
		{"autolink", "<https://example.org>",
			"<p><a href=\"https://example.org\">https://example.org</a></p>\n"},
		{"email autolink", "<a@b.com>", "<p><a href=\"mailto:a@b.com\">a@b.com</a></p>\n"},
		{"escapes", `\*lit\*`, "<p>*lit*</p>\n"},
		{"html escaping", `1 < 2 & "q"`, "<p>1 &lt; 2 &amp; &quot;q&quot;</p>\n"},
		{"entity passthrough", "&copy; &#169;", "<p>&copy; &#169;</p>\n"},
		{"hard break", "a  \nb", "<p>a<br>\nb</p>\n"},
		{"backslash break", "a\\\nb", "<p>a<br>\nb</p>\n"},
		{"inline html", "press <kbd>x</kbd>", "<p>press <kbd>x</kbd></p>\n"},
		{"tight list", "- a\n- b", "<ul>\n<li>a</li>\n<li>b</li>\n</ul>\n"},
		{"ordered start", "3. a\n4. b", "<ol start=\"3\">\n<li>a</li>\n<li>b</li>\n</ol>\n"},
		{"nested list", "- a\n  - b",
			"<ul>\n<li>a\n<ul>\n<li>b</li>\n</ul></li>\n</ul>\n"},
		{"loose list", "- a\n\n- b",
			"<ul>\n<li><p>a</p></li>\n<li><p>b</p></li>\n</ul>\n"},
		{"blockquote", "> quoted", "<blockquote>\n<p>quoted</p>\n</blockquote>\n"},
		{"nested blockquote", "> a\n> > b",
			"<blockquote>\n<p>a</p>\n<blockquote>\n<p>b</p>\n</blockquote>\n</blockquote>\n"},
		{"fenced code", "```go\nx\n```",
			"<pre><code class=\"language-go\">x\n</code></pre>\n"},
		{"fence escapes html", "```\n<b>&\n```",
			"<pre><code>&lt;b&gt;&amp;\n</code></pre>\n"},
		{"indented code", "    code\n    more",
			"<pre><code>code\nmore\n</code></pre>\n"},
		{"hr", "---", "<hr>\n"},
		{"hr stars", "* * *", "<hr>\n"},
		{"table", "| a | b |\n|---|--:|\n| 1 | 2 |",
			"<table>\n<thead>\n<tr><th>a</th><th style=\"text-align:right\">b</th></tr>\n</thead>\n" +
				"<tbody>\n<tr><td>1</td><td style=\"text-align:right\">2</td></tr>\n</tbody>\n</table>\n"},
		{"escaped pipe in table", "| a\\|b |\n|---|\n| c |",
			"<table>\n<thead>\n<tr><th>a|b</th></tr>\n</thead>\n" +
				"<tbody>\n<tr><td>c</td></tr>\n</tbody>\n</table>\n"},
		{"html block", "<div>\nraw & kept\n</div>", "<div>\nraw & kept\n</div>\n"},
		{"duplicate slugs", "# Dup\n# Dup",
			"<h1 id=\"dup\">Dup</h1>\n<h1 id=\"dup-1\">Dup</h1>\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := markdownBody(tc.md)
			if got != tc.want {
				t.Errorf("markdownBody(%q)\n got: %q\nwant: %q", tc.md, got, tc.want)
			}
		})
	}
}

func TestTitleExtraction(t *testing.T) {
	_, title := markdownBody("# My *Fancy* Title\n\ntext")
	if title != "My Fancy Title" {
		t.Errorf("title = %q, want %q", title, "My Fancy Title")
	}
	_, title = markdownBody("## only h2 here")
	if title != "" {
		t.Errorf("title = %q, want empty (no h1)", title)
	}
}

func TestConvertDocument(t *testing.T) {
	doc := convert("# Hi\n\nbody", "fallback")
	for _, want := range []string{"<!DOCTYPE html>", "<title>Hi</title>", "<style>",
		"prefers-color-scheme", "<h1 id=\"hi\">Hi</h1>"} {
		if !strings.Contains(doc, want) {
			t.Errorf("document missing %q", want)
		}
	}
	doc = convert("no heading", "fall<back")
	if !strings.Contains(doc, "<title>fall&lt;back</title>") {
		t.Errorf("fallback title not escaped/used:\n%s", doc[:200])
	}
}

func TestOutputPath(t *testing.T) {
	cases := []struct{ input, flag, want string }{
		{"foobar.md", "", "foobar.html"},
		{"doc.markdown", "", "doc.html"},
		{"plain", "", "plain.html"},
		{"foobar.md", "mumble.html", "mumble.html"},
		{"foobar.md", "-", "-"},
		{"-", "", "-"},
	}
	for _, tc := range cases {
		if got := outputPath(tc.input, tc.flag); got != tc.want {
			t.Errorf("outputPath(%q, %q) = %q, want %q", tc.input, tc.flag, got, tc.want)
		}
	}
}

func FuzzMarkdownBody(f *testing.F) {
	seeds := []string{
		"# h\n\n*a* [b](c) `d`\n\n- 1\n- 2\n\n```\nx\n```\n",
		"| a |\n|---|\n| *b* |\n",
		"> q\n> > qq\n\n***\n\n1. x\n",
		"**unclosed *runs ~~and `ticks\n[bad](link \"t\n",
		"\\* \\` \\[ &amp; <not a tag <em>tag</em>\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		markdownBody(s) // must not panic or hang
	})
}

// ---------------------------------------------------------------------------
// Official spec suites. demark is intentionally simplified, so it does not
// pass every CommonMark example; the pass lists in testdata/ pin exactly
// which examples pass. The test fails if a previously-passing example
// regresses. Run `go test -update` to regenerate the lists.

type specCase struct {
	Markdown string `json:"markdown"`
	HTML     string `json:"html"`
	Example  int    `json:"example"`
	Section  string `json:"section"`
}

func TestCommonMarkSpec(t *testing.T) {
	data, err := os.ReadFile("testdata/spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []specCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	runSpec(t, cases, "testdata/commonmark_pass.txt")
}

func TestGFMSpec(t *testing.T) {
	cases := parseSpecTxt(t, "testdata/gfm_spec.txt", map[string]bool{
		"Tables (extension)":        true,
		"Strikethrough (extension)": true,
	})
	if len(cases) == 0 {
		t.Fatal("no GFM extension examples parsed")
	}
	runSpec(t, cases, "testdata/gfm_pass.txt")
}

func runSpec(t *testing.T, cases []specCase, passFile string) {
	expected := loadPassList(t, passFile)
	var passing []specCase
	newlyPassing := 0
	for _, c := range cases {
		body, _ := markdownBody(c.Markdown)
		ok := normalizeHTML(body) == normalizeHTML(c.HTML)
		if ok {
			passing = append(passing, c)
		}
		if *update {
			continue
		}
		switch {
		case expected[c.Example] && !ok:
			t.Errorf("REGRESSION example %d (%s)\nmarkdown: %q\nwant: %q\ngot:  %q",
				c.Example, c.Section, c.Markdown,
				normalizeHTML(c.HTML), normalizeHTML(body))
		case !expected[c.Example] && ok:
			newlyPassing++
		}
	}
	t.Logf("%d/%d examples pass (%s)", len(passing), len(cases), passFile)
	if newlyPassing > 0 {
		t.Logf("%d newly passing examples not in %s; run `go test -update` to record them",
			newlyPassing, passFile)
	}
	if *update {
		writePassList(t, passFile, passing)
	} else if expected == nil {
		t.Errorf("missing %s; run `go test -update` to create it", passFile)
	}
}

func loadPassList(t *testing.T, path string) map[int]bool {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	m := map[int]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("%s: bad line %q", path, line)
		}
		m[n] = true
	}
	return m
}

func writePassList(t *testing.T, path string, passing []specCase) {
	sort.Slice(passing, func(i, j int) bool { return passing[i].Example < passing[j].Example })
	var b strings.Builder
	for _, c := range passing {
		fmt.Fprintf(&b, "%d\t%s\n", c.Example, c.Section)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%d examples)", path, len(passing))
}

// parseSpecTxt extracts examples from the spec.txt format used by the GFM
// spec: 32-backtick "example" fences with markdown and html separated by a
// "." line. Tabs are encoded as "→". Example numbers count every example in
// the file, matching the official numbering; sections filters which are kept.
func parseSpecTxt(t *testing.T, path string, sections map[string]bool) []specCase {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fence := strings.Repeat("`", 32)
	headingRe := regexp.MustCompile(`^#{1,6} +(.*?) *$`)
	lines := strings.Split(string(data), "\n")
	var cases []specCase
	section := ""
	num := 0
	for i := 0; i < len(lines); i++ {
		if m := headingRe.FindStringSubmatch(lines[i]); m != nil {
			section = m[1]
			continue
		}
		if !strings.HasPrefix(lines[i], fence+" example") {
			continue
		}
		num++
		i++
		var md, html []string
		cur := &md
		for i < len(lines) && !strings.HasPrefix(lines[i], fence) {
			if lines[i] == "." {
				cur = &html
			} else {
				*cur = append(*cur, lines[i])
			}
			i++
		}
		if sections[section] {
			join := func(ls []string) string {
				if len(ls) == 0 {
					return ""
				}
				return strings.ReplaceAll(strings.Join(ls, "\n"), "→", "\t") + "\n"
			}
			cases = append(cases, specCase{
				Markdown: join(md), HTML: join(html), Example: num, Section: section,
			})
		}
	}
	return cases
}

// normalizeHTML reduces insignificant differences between demark's output
// and the spec's expected HTML: demark's added heading ids, XHTML-style
// self-closing tags, quote escaping in text, align vs. text-align styling,
// and whitespace between tags.
var (
	idAttrRe     = regexp.MustCompile(` id="[^"]*"`)
	alignAttrRe  = regexp.MustCompile(` align="(left|right|center)"`)
	interTagWSRe = regexp.MustCompile(`>\s+<`)
)

func normalizeHTML(s string) string {
	s = idAttrRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, " />", ">")
	s = strings.ReplaceAll(s, "/>", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = alignAttrRe.ReplaceAllString(s, ` style="text-align:$1"`)
	s = interTagWSRe.ReplaceAllString(s, "><")
	return strings.TrimSpace(s)
}
