// demark converts Markdown files to standalone HTML documents.
//
// Usage:
//
//	demark foobar.md              writes foobar.html
//	demark foobar.md -o out.html  writes out.html
//	demark foobar.md -o -         writes to stdout
//	demark -                      reads stdin, writes stdout
//
// Supports CommonMark-style headings (ATX and setext), paragraphs, emphasis,
// inline code, fenced and indented code blocks, blockquotes, nested ordered
// and unordered lists, GFM tables and strikethrough, links, images,
// autolinks, horizontal rules, hard breaks, and backslash escapes.
// Inline and block raw HTML is passed through. Reference-style links are
// not supported.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// CLI

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: demark <input.md> [-o <output.html>]

Converts a Markdown file to a standalone HTML document with built-in styling.

  -o <file>    write output to <file> ("-" for stdout)
  --open       open the generated HTML with the system's default handler
  --install    (Windows) add Explorer right-click entries for .md files:
               "Convert to HTML" and "View as HTML"
  --uninstall  (Windows) remove the Explorer right-click entries
  -h, --help   show this help

Without -o, foobar.md becomes foobar.html. Use "-" as the input file to
read Markdown from stdin (output then defaults to stdout).
`)
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "demark: "+msg)
	os.Exit(1)
}

func main() {
	input, output := "", ""
	openAfter, install, uninstall := false, false, false
	args := os.Args[1:]
	for j := 0; j < len(args); j++ {
		a := args[j]
		switch {
		case a == "-o" || a == "--output":
			j++
			if j >= len(args) {
				fail("missing argument for " + a)
			}
			output = args[j]
		case strings.HasPrefix(a, "-o="):
			output = a[len("-o="):]
		case strings.HasPrefix(a, "--output="):
			output = a[len("--output="):]
		case a == "--open":
			openAfter = true
		case a == "--install":
			install = true
		case a == "--uninstall":
			uninstall = true
		case a == "-h" || a == "--help":
			usage(os.Stdout)
			return
		case a == "-" || !strings.HasPrefix(a, "-"):
			if input != "" {
				fail("multiple input files given")
			}
			input = a
		default:
			fail("unknown flag: " + a)
		}
	}

	if install || uninstall {
		if err := setupShellMenu(uninstall); err != nil {
			fail(err.Error())
		}
		if uninstall {
			fmt.Println("demark: removed Explorer context-menu entries for .md files")
		} else {
			fmt.Println("demark: installed Explorer context-menu entries for .md files")
		}
		return
	}

	if input == "" {
		usage(os.Stderr)
		os.Exit(2)
	}

	var src []byte
	var err error
	if input == "-" {
		src, err = io.ReadAll(os.Stdin)
	} else {
		src, err = os.ReadFile(input)
	}
	if err != nil {
		fail(err.Error())
	}

	fallbackTitle := "Document"
	if input != "-" {
		base := filepath.Base(input)
		fallbackTitle = strings.TrimSuffix(base, filepath.Ext(base))
	}

	output = outputPath(input, output)
	if output == input && output != "-" {
		fail("output would overwrite input file")
	}

	doc := convert(string(src), fallbackTitle)

	if output == "-" {
		if openAfter {
			fail("--open cannot be combined with stdout output")
		}
		fmt.Print(doc)
		return
	}
	if err := os.WriteFile(output, []byte(doc), 0o644); err != nil {
		fail(err.Error())
	}
	if openAfter {
		if err := openWithHandler(output); err != nil {
			fail("opening " + output + ": " + err.Error())
		}
	}
}

// openWithHandler opens path with the operating system's default handler
// for its file type (e.g. the default browser for .html).
func openWithHandler(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// ---------------------------------------------------------------------------
// Windows Explorer integration

// shellMenuCommands returns the reg.exe invocations that add (or remove)
// per-user Explorer context-menu verbs for .md files. Registering under
// HKCU\Software\Classes\SystemFileAssociations applies to the extension
// regardless of which application owns the .md association, and needs no
// administrator rights.
func shellMenuCommands(exe string, remove bool) [][]string {
	verbs := []struct{ key, label, args string }{
		{"demark.convert", "Convert to HTML", `"%1"`},
		{"demark.view", "View as HTML", `"%1" --open`},
	}
	var cmds [][]string
	for _, ext := range []string{".md", ".markdown"} {
		base := `HKCU\Software\Classes\SystemFileAssociations\` + ext + `\shell\`
		for _, v := range verbs {
			key := base + v.key
			if remove {
				cmds = append(cmds, []string{"reg", "delete", key, "/f"})
				continue
			}
			cmds = append(cmds,
				[]string{"reg", "add", key, "/ve", "/d", v.label, "/f"},
				[]string{"reg", "add", key, "/v", "Icon", "/d", exe, "/f"},
				[]string{"reg", "add", key + `\command`, "/ve",
					"/d", `"` + exe + `" ` + v.args, "/f"})
		}
	}
	return cmds
}

func setupShellMenu(remove bool) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("--install and --uninstall are only supported on Windows")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, c := range shellMenuCommands(exe, remove) {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v: %s", strings.Join(c, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Document assembly

// outputPath resolves the output file name: an explicit -o value wins,
// stdin input defaults to stdout, and foo.md becomes foo.html.
func outputPath(input, output string) string {
	if output != "" {
		return output
	}
	if input == "-" {
		return "-"
	}
	base := input
	switch strings.ToLower(filepath.Ext(input)) {
	case ".md", ".markdown", ".mdown", ".mkd", ".mkdn":
		base = strings.TrimSuffix(input, filepath.Ext(input))
	}
	return base + ".html"
}

// markdownBody converts Markdown source to body HTML, also returning the
// text of the first h1 (already HTML-escaped) for use as a document title.
func markdownBody(src string) (body, title string) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	c := &conv{ids: map[string]int{}}
	body = c.blocks(strings.Split(src, "\n"), false)
	return body, c.title
}

func convert(src, fallbackTitle string) string {
	body, title := markdownBody(src)
	if title == "" {
		title = escapeHTML(fallbackTitle)
	}
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>" + title + "</title>\n")
	b.WriteString("<style>\n" + stylesheet + "</style>\n")
	b.WriteString("</head>\n<body>\n<main>\n")
	b.WriteString(body)
	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Block-level parsing

var (
	atxRe          = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)
	trailingHashRe = regexp.MustCompile(`[ \t]+#+$`)
	fenceRe        = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})[ \t]*(.*)$")
	listRe         = regexp.MustCompile(`^( *)([-+*]|\d{1,9}[.)])(?:[ \t]+(.*))?$`)
	setext1Re      = regexp.MustCompile(`^ {0,3}=+[ \t]*$`)
	setext2Re      = regexp.MustCompile(`^ {0,3}-+[ \t]*$`)
	tableSepRe     = regexp.MustCompile(`^ *\|? *:?-+:? *(\| *:?-+:? *)*\|? *$`)
	htmlBlockRe    = regexp.MustCompile(`^ {0,3}<(?:/?[A-Za-z][A-Za-z0-9-]*(?:[ \t>]|/>|$)|!--)`)
	tagStripRe     = regexp.MustCompile(`<[^>]*>`)
	entityStripRe  = regexp.MustCompile(`&#?[A-Za-z0-9]+;`)
)

// isHR reports whether line is a thematic break: three or more of the same
// -, * or _ character, optionally space-separated, indented at most 3.
func isHR(line string) bool {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || t == "" {
		return false
	}
	ch := t[0]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	count := 0
	for j := 0; j < len(t); j++ {
		switch t[j] {
		case ch:
			count++
		case ' ', '\t':
		default:
			return false
		}
	}
	return count >= 3
}

type conv struct {
	title string // escaped text of the first h1, used as <title>
	ids   map[string]int
}

// blocks renders a sequence of lines as block-level HTML. When tight is
// true (tight list items), paragraphs are emitted without <p> wrappers.
func (c *conv) blocks(lines []string, tight bool) string {
	var out strings.Builder
	var para []string

	flush := func() {
		if len(para) == 0 {
			return
		}
		inner := renderInline(strings.Join(para, "\n"))
		para = nil
		if tight {
			out.WriteString(inner + "\n")
		} else {
			out.WriteString("<p>" + inner + "</p>\n")
		}
	}

	i := 0
	for i < len(lines) {
		line := expandTabs(lines[i])
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			i++
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))

		// Setext headings close an open paragraph.
		if len(para) > 0 && (setext1Re.MatchString(line) || setext2Re.MatchString(line)) {
			level := 2
			if trimmed[0] == '=' {
				level = 1
			}
			txt := strings.Join(para, "\n")
			para = nil
			c.heading(&out, level, txt)
			i++
			continue
		}

		// Fenced code blocks.
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			flush()
			fenceIndent, fence, info := len(m[1]), m[2], strings.TrimSpace(m[3])
			i++
			var code []string
			for i < len(lines) {
				l := expandTabs(lines[i])
				if isCloseFence(strings.TrimSpace(l), fence) {
					i++
					break
				}
				for k := 0; k < fenceIndent && len(l) > 0 && l[0] == ' '; k++ {
					l = l[1:]
				}
				code = append(code, l)
				i++
			}
			cls := ""
			if lang := strings.Fields(info + " "); len(lang) > 0 && lang[0] != "" {
				cls = ` class="language-` + escapeHTML(lang[0]) + `"`
			}
			out.WriteString("<pre><code" + cls + ">" + escapeHTML(strings.Join(code, "\n")))
			if len(code) > 0 {
				out.WriteString("\n")
			}
			out.WriteString("</code></pre>\n")
			continue
		}

		// ATX headings.
		if m := atxRe.FindStringSubmatch(line); m != nil {
			flush()
			txt := trailingHashRe.ReplaceAllString(strings.TrimSpace(m[2]), "")
			c.heading(&out, len(m[1]), txt)
			i++
			continue
		}

		// Horizontal rules (checked before lists: "* * *" matches both).
		if isHR(line) {
			flush()
			out.WriteString("<hr>\n")
			i++
			continue
		}

		// Blockquotes. Check line[indent] rather than trimmed[0]: TrimSpace
		// also strips \f and \v, which the consuming loop below does not,
		// and the two must agree or the loop consumes nothing.
		if indent <= 3 && indent < len(line) && line[indent] == '>' {
			flush()
			var q []string
			for i < len(lines) {
				l := expandTabs(lines[i])
				lt := strings.TrimLeft(l, " ")
				switch {
				case len(l)-len(lt) <= 3 && strings.HasPrefix(lt, ">"):
					body := strings.TrimPrefix(lt[1:], " ")
					q = append(q, body)
					i++
				case strings.TrimSpace(l) != "" && len(q) > 0 &&
					strings.TrimSpace(q[len(q)-1]) != "" && !startsBlock(l):
					q = append(q, l) // lazy paragraph continuation
					i++
				default:
					goto quoteDone
				}
			}
		quoteDone:
			out.WriteString("<blockquote>\n" + c.blocks(q, false) + "</blockquote>\n")
			continue
		}

		// Lists.
		if _, marker, rest, _, ok := markerMatch(line); ok && indent <= 3 {
			interrupts := true
			if len(para) > 0 {
				// Only "1." lists and non-empty items may interrupt a paragraph.
				ordered := marker[0] >= '0' && marker[0] <= '9'
				if rest == "" || (ordered && marker[:len(marker)-1] != "1") {
					interrupts = false
				}
			}
			if interrupts {
				flush()
				html, next := c.list(lines, i)
				out.WriteString(html)
				i = next
				continue
			}
		}

		// Indented code blocks.
		if indent >= 4 && len(para) == 0 {
			var code []string
			for i < len(lines) {
				l := expandTabs(lines[i])
				if strings.TrimSpace(l) == "" {
					code = append(code, "")
					i++
					continue
				}
				if len(l)-len(strings.TrimLeft(l, " ")) < 4 {
					break
				}
				code = append(code, l[4:])
				i++
			}
			for len(code) > 0 && code[len(code)-1] == "" {
				code = code[:len(code)-1]
			}
			out.WriteString("<pre><code>" + escapeHTML(strings.Join(code, "\n")) + "\n</code></pre>\n")
			continue
		}

		// Tables (header row, then a |---|---| separator).
		if len(para) == 0 && strings.Contains(line, "|") && i+1 < len(lines) {
			sep := expandTabs(lines[i+1])
			if strings.Contains(sep, "|") && strings.Contains(sep, "-") && tableSepRe.MatchString(sep) {
				header := splitRow(line)
				aligns := parseAligns(sep)
				if len(header) == len(aligns) {
					i += 2
					var rows [][]string
					for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
						l := expandTabs(lines[i])
						mInd, _, _, _, mOK := markerMatch(l)
						if startsBlock(l) || (mOK && mInd <= 3) {
							break // a new block ends the table
						}
						rows = append(rows, splitRow(l))
						i++
					}
					out.WriteString(renderTable(header, aligns, rows))
					continue
				}
			}
		}

		// Raw HTML blocks: pass through verbatim until a blank line.
		if len(para) == 0 && htmlBlockRe.MatchString(line) {
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
				out.WriteString(lines[i] + "\n")
				i++
			}
			continue
		}

		// Paragraph text (keep trailing spaces: they encode hard breaks).
		para = append(para, strings.TrimLeft(line, " "))
		i++
	}
	flush()
	return out.String()
}

func (c *conv) heading(out *strings.Builder, level int, txt string) {
	inner := renderInline(strings.TrimSpace(txt))
	plain := tagStripRe.ReplaceAllString(inner, "")
	if level == 1 && c.title == "" {
		c.title = plain
	}
	id := c.slug(entityStripRe.ReplaceAllString(plain, ""))
	fmt.Fprintf(out, "<h%d id=%q>%s</h%d>\n", level, id, inner, level)
}

func (c *conv) slug(plain string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(plain) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || r == '\t':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		s = "section"
	}
	if n, seen := c.ids[s]; seen {
		c.ids[s] = n + 1
		s = fmt.Sprintf("%s-%d", s, n+1)
	} else {
		c.ids[s] = 0
	}
	return s
}

// markerMatch reports whether line begins a list item, returning its leading
// indent, the marker text, the item's first-line content, and the column at
// which item content starts.
func markerMatch(line string) (indent int, marker, rest string, contentIndent int, ok bool) {
	loc := listRe.FindStringSubmatchIndex(line)
	if loc == nil {
		return 0, "", "", 0, false
	}
	indent = loc[3] - loc[2]
	marker = line[loc[4]:loc[5]]
	if loc[6] >= 0 {
		rest = line[loc[6]:loc[7]]
		contentIndent = loc[6]
	} else {
		contentIndent = loc[5] + 1
	}
	return indent, marker, rest, contentIndent, true
}

func (c *conv) list(lines []string, i int) (string, int) {
	first := expandTabs(lines[i])
	_, marker, rest, contentIndent, _ := markerMatch(first)
	ordered := marker[0] >= '0' && marker[0] <= '9'
	typeByte := marker[0] // bullet char, or the . / ) delimiter when ordered
	start := 1
	if ordered {
		typeByte = marker[len(marker)-1]
		start, _ = strconv.Atoi(marker[:len(marker)-1])
	}

	var items [][]string
	cur := []string{rest}
	loose := false
	blankPending := false
	i++
	for i < len(lines) {
		raw := expandTabs(lines[i])
		if strings.TrimSpace(raw) == "" {
			blankPending = true
			cur = append(cur, "")
			i++
			continue
		}
		ind := len(raw) - len(strings.TrimLeft(raw, " "))
		if isHR(raw) && ind < contentIndent {
			break // a horizontal rule ends the list
		}
		mInd, mMarker, mRest, mCI, mOK := markerMatch(raw)
		if mOK && mInd < contentIndent {
			mOrdered := mMarker[0] >= '0' && mMarker[0] <= '9'
			mType := mMarker[0]
			if mOrdered {
				mType = mMarker[len(mMarker)-1]
			}
			if mOrdered != ordered || mType != typeByte {
				break // a different list type starts a new list
			}
			if blankPending {
				loose = true
			}
			blankPending = false
			items = append(items, cur)
			cur = []string{mRest}
			contentIndent = mCI
			i++
			continue
		}
		if ind >= contentIndent {
			if blankPending {
				loose = true
				blankPending = false
			}
			cur = append(cur, raw[contentIndent:])
			i++
			continue
		}
		if !blankPending && !mOK && !startsBlock(raw) {
			cur = append(cur, strings.TrimLeft(raw, " ")) // lazy continuation
			i++
			continue
		}
		break
	}
	items = append(items, cur)

	tag, attr := "ul", ""
	if ordered {
		tag = "ol"
		if start != 1 {
			attr = fmt.Sprintf(` start="%d"`, start)
		}
	}
	var b strings.Builder
	b.WriteString("<" + tag + attr + ">\n")
	for _, item := range items {
		for len(item) > 0 && strings.TrimSpace(item[len(item)-1]) == "" {
			item = item[:len(item)-1]
		}
		inner := strings.TrimRight(c.blocks(item, !loose), "\n")
		b.WriteString("<li>" + inner + "</li>\n")
	}
	b.WriteString("</" + tag + ">\n")
	return b.String(), i
}

// startsBlock reports whether line begins a non-paragraph block, ending lazy
// continuation of blockquotes and list items.
func startsBlock(line string) bool {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return false
	}
	if strings.HasPrefix(t, ">") {
		return true
	}
	return atxRe.MatchString(line) || isHR(line) || fenceRe.MatchString(line)
}

func isCloseFence(trimmed, fence string) bool {
	n := 0
	for n < len(trimmed) && trimmed[n] == fence[0] {
		n++
	}
	return n >= len(fence) && strings.TrimSpace(trimmed[n:]) == ""
}

// ---------------------------------------------------------------------------
// Tables

func splitRow(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	var cells []string
	var cur strings.Builder
	for j := 0; j < len(s); j++ {
		switch {
		case s[j] == '\\' && j+1 < len(s):
			// GFM unescapes \| at row-split time (even inside code spans);
			// other escapes are left for the inline parser.
			if s[j+1] != '|' {
				cur.WriteByte(s[j])
			}
			cur.WriteByte(s[j+1])
			j++
		case s[j] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(s[j])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func parseAligns(sep string) []string {
	cells := splitRow(sep)
	aligns := make([]string, len(cells))
	for k, cell := range cells {
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			aligns[k] = "center"
		case right:
			aligns[k] = "right"
		case left:
			aligns[k] = "left"
		}
	}
	return aligns
}

func renderTable(header, aligns []string, rows [][]string) string {
	cell := func(tag, text string, col int) string {
		style := ""
		if col < len(aligns) && aligns[col] != "" {
			style = ` style="text-align:` + aligns[col] + `"`
		}
		return "<" + tag + style + ">" + renderInline(text) + "</" + tag + ">"
	}
	var b strings.Builder
	b.WriteString("<table>\n<thead>\n<tr>")
	for k, h := range header {
		b.WriteString(cell("th", h, k))
	}
	b.WriteString("</tr>\n</thead>\n")
	if len(rows) > 0 {
		b.WriteString("<tbody>\n")
		for _, row := range rows {
			b.WriteString("<tr>")
			for k := range header {
				text := ""
				if k < len(row) {
					text = row[k]
				}
				b.WriteString(cell("td", text, k))
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</tbody>\n")
	}
	b.WriteString("</table>\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Inline parsing

var (
	autolinkRe = regexp.MustCompile(
		`^<([A-Za-z][A-Za-z0-9+.-]*://[^\s<>]+|mailto:[^\s<>]+|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,})>`)
	htmlTagRe     = regexp.MustCompile(`^</?[A-Za-z][A-Za-z0-9-]*(?:\s+[^<>]*)?/?>`)
	htmlCommentRe = regexp.MustCompile(`^<!--[\s\S]*?-->`)
	entityRe      = regexp.MustCompile(`^&(?:[A-Za-z][A-Za-z0-9]{1,31}|#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6});`)
)

func renderInline(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\':
			if i+1 < len(s) {
				switch {
				case s[i+1] == '\n':
					b.WriteString("<br>\n")
					i += 2
					continue
				case isPunct(s[i+1]):
					b.WriteString(escapeHTML(string(s[i+1])))
					i += 2
					continue
				}
			}
			b.WriteByte('\\')
			i++
		case c == '`':
			n := runLen(s, i)
			if end := findCodeClose(s, i+n, n); end >= 0 {
				code := strings.ReplaceAll(s[i+n:end], "\n", " ")
				if len(code) >= 2 && code[0] == ' ' && code[len(code)-1] == ' ' &&
					strings.TrimSpace(code) != "" {
					code = code[1 : len(code)-1]
				}
				b.WriteString("<code>" + escapeHTML(code) + "</code>")
				i = end + n
			} else {
				b.WriteString(s[i : i+n])
				i += n
			}
		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			if html, n := parseLink(s, i+1, true); n > 0 {
				b.WriteString(html)
				i += 1 + n
			} else {
				b.WriteByte('!')
				i++
			}
		case c == '[':
			if html, n := parseLink(s, i, false); n > 0 {
				b.WriteString(html)
				i += n
			} else {
				b.WriteByte('[')
				i++
			}
		case c == '*' || c == '_':
			html, n := emphasis(s, i)
			b.WriteString(html)
			i += n
		case c == '~' && i+1 < len(s) && s[i+1] == '~':
			if k, _ := findDelim(s, i+2, '~', 2); k > i+2 {
				b.WriteString("<del>" + renderInline(s[i+2:k]) + "</del>")
				i = k + 2
			} else {
				b.WriteString("~~")
				i += 2
			}
		case c == '<':
			rest := s[i:]
			if m := autolinkRe.FindStringSubmatch(rest); m != nil {
				href := m[1]
				if strings.Contains(href, "@") && !strings.Contains(href, ":") {
					href = "mailto:" + href
				}
				b.WriteString(`<a href="` + escapeHTML(href) + `">` + escapeHTML(m[1]) + `</a>`)
				i += len(m[0])
			} else if m := htmlTagRe.FindString(rest); m != "" {
				b.WriteString(m)
				i += len(m)
			} else if m := htmlCommentRe.FindString(rest); m != "" {
				b.WriteString(m)
				i += len(m)
			} else {
				b.WriteString("&lt;")
				i++
			}
		case c == '&':
			if m := entityRe.FindString(s[i:]); m != "" {
				b.WriteString(m)
				i += len(m)
			} else {
				b.WriteString("&amp;")
				i++
			}
		case c == '>':
			b.WriteString("&gt;")
			i++
		case c == '"':
			b.WriteString("&quot;")
			i++
		case c == ' ':
			j := i
			for j < len(s) && s[j] == ' ' {
				j++
			}
			if j-i >= 2 && j < len(s) && s[j] == '\n' {
				b.WriteString("<br>\n") // hard break: two+ trailing spaces
				i = j + 1
			} else {
				b.WriteString(s[i:j])
				i = j
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// emphasis handles a run of * or _ at s[i], returning rendered HTML and the
// number of bytes consumed.
func emphasis(s string, i int) (string, int) {
	c := s[i]
	n := runLen(s, i)
	// An opener must be followed by non-space; _ may not open intraword.
	if i+n >= len(s) || isSpaceByte(s[i+n]) || (c == '_' && i > 0 && isAlnum(s[i-1])) {
		return s[i : i+n], n
	}
	if n >= 2 {
		if k, kl := findDelim(s, i+n, c, 2); k >= 0 {
			inner := renderInline(s[i+n : k])
			if n >= 3 && kl >= 3 {
				return strings.Repeat(string(c), n-3) +
					"<em><strong>" + inner + "</strong></em>", k + 3 - i
			}
			return strings.Repeat(string(c), n-2) +
				"<strong>" + inner + "</strong>", k + 2 - i
		}
	}
	if k, _ := findDelim(s, i+n, c, 1); k >= 0 {
		return strings.Repeat(string(c), n-1) +
			"<em>" + renderInline(s[i+n:k]) + "</em>", k + 1 - i
	}
	return s[i : i+n], n
}

// findDelim finds the next closing delimiter run of d with at least minLen
// characters, skipping escapes and code spans. Returns its index and length.
func findDelim(s string, from int, d byte, minLen int) (int, int) {
	j := from
	for j < len(s) {
		switch s[j] {
		case '\\':
			j += 2
			continue
		case '`':
			n := runLen(s, j)
			if end := findCodeClose(s, j+n, n); end >= 0 {
				j = end + n
			} else {
				j += n
			}
			continue
		}
		if s[j] == d {
			n := runLen(s, j)
			closes := j > 0 && !isSpaceByte(s[j-1]) && n >= minLen
			if closes && d == '_' && j+n < len(s) && isAlnum(s[j+n]) {
				closes = false // _ may not close intraword
			}
			if closes && minLen == 1 && n >= 2 {
				closes = false // likely a nested ** run; skip it
			}
			if closes {
				return j, n
			}
			j += n
			continue
		}
		j++
	}
	return -1, 0
}

func findCodeClose(s string, from, n int) int {
	for j := from; j < len(s); j++ {
		if s[j] == '`' {
			l := runLen(s, j)
			if l == n {
				return j
			}
			j += l - 1
		}
	}
	return -1
}

// parseLink parses [text](url "title") starting at the '[' at s[i].
// Returns rendered HTML and bytes consumed, or ("", 0) if it doesn't parse.
func parseLink(s string, i int, isImage bool) (string, int) {
	depth := 0
	end := -1
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end = j
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 || end+1 >= len(s) || s[end+1] != '(' {
		return "", 0
	}
	text := s[i+1 : end]

	k := end + 2
	for k < len(s) && s[k] == ' ' {
		k++
	}
	var url string
	if k < len(s) && s[k] == '<' {
		c := strings.IndexByte(s[k:], '>')
		if c < 0 {
			return "", 0
		}
		url = s[k+1 : k+c]
		k += c + 1
	} else {
		st, parens := k, 0
		for k < len(s) {
			ch := s[k]
			if ch == '\\' && k+1 < len(s) {
				k += 2
				continue
			}
			if ch == ' ' || (ch == ')' && parens == 0) {
				break
			}
			if ch == '(' {
				parens++
			} else if ch == ')' {
				parens--
			}
			k++
		}
		url = s[st:k]
	}
	for k < len(s) && s[k] == ' ' {
		k++
	}
	title := ""
	if k < len(s) && (s[k] == '"' || s[k] == '\'') {
		q := s[k]
		c := strings.IndexByte(s[k+1:], q)
		if c < 0 {
			return "", 0
		}
		title = s[k+1 : k+1+c]
		k += c + 2
		for k < len(s) && s[k] == ' ' {
			k++
		}
	}
	if k >= len(s) || s[k] != ')' {
		return "", 0
	}
	k++

	href := escapeHTML(unescapeMD(url))
	titleAttr := ""
	if title != "" {
		titleAttr = ` title="` + escapeHTML(unescapeMD(title)) + `"`
	}
	if isImage {
		alt := tagStripRe.ReplaceAllString(renderInline(text), "")
		return `<img src="` + href + `" alt="` + alt + `"` + titleAttr + `>`, k - i
	}
	return `<a href="` + href + `"` + titleAttr + `>` + renderInline(text) + `</a>`, k - i
}

// ---------------------------------------------------------------------------
// Small helpers

var htmlEscaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func escapeHTML(s string) string { return htmlEscaper.Replace(s) }

func unescapeMD(s string) string {
	var b strings.Builder
	for j := 0; j < len(s); j++ {
		if s[j] == '\\' && j+1 < len(s) && isPunct(s[j+1]) {
			j++
		}
		b.WriteByte(s[j])
	}
	return b.String()
}

func runLen(s string, i int) int {
	j := i
	for j < len(s) && s[j] == s[i] {
		j++
	}
	return j - i
}

func isPunct(b byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", b) >= 0
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' || b == '\n' }

func isAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
		} else {
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Baked-in stylesheet

const stylesheet = `:root {
  --bg: #ffffff;
  --fg: #1f2328;
  --muted: #59636e;
  --border: #d1d9e0;
  --code-bg: #f6f8fa;
  --link: #0969da;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0d1117;
    --fg: #e6edf3;
    --muted: #9198a1;
    --border: #3d444d;
    --code-bg: #161b22;
    --link: #4493f8;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--fg);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial,
    sans-serif, "Apple Color Emoji", "Segoe UI Emoji";
  font-size: 16px;
  line-height: 1.6;
  -webkit-text-size-adjust: 100%;
}
main {
  max-width: 48rem;
  margin: 0 auto;
  padding: 3rem 1.5rem 6rem;
}
main > :first-child { margin-top: 0; }
h1, h2, h3, h4, h5, h6 {
  margin: 1.6em 0 0.6em;
  line-height: 1.25;
  font-weight: 600;
}
h1 { font-size: 2em; border-bottom: 1px solid var(--border); padding-bottom: 0.3em; }
h2 { font-size: 1.5em; border-bottom: 1px solid var(--border); padding-bottom: 0.3em; }
h3 { font-size: 1.25em; }
h4 { font-size: 1em; }
h5 { font-size: 0.875em; }
h6 { font-size: 0.85em; color: var(--muted); }
p, ul, ol, blockquote, pre, table { margin: 0 0 1em; }
a { color: var(--link); text-decoration: none; }
a:hover { text-decoration: underline; }
code, pre {
  font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas,
    "Liberation Mono", monospace;
}
code {
  background: var(--code-bg);
  padding: 0.2em 0.4em;
  border-radius: 6px;
  font-size: 85%;
}
pre {
  background: var(--code-bg);
  padding: 1rem;
  border-radius: 6px;
  overflow-x: auto;
  line-height: 1.45;
  font-size: 85%;
}
pre code { background: none; padding: 0; font-size: 100%; }
blockquote {
  border-left: 0.25em solid var(--border);
  padding: 0 1em;
  color: var(--muted);
}
blockquote > :last-child { margin-bottom: 0; }
ul, ol { padding-left: 2em; }
li + li { margin-top: 0.25em; }
li > ul, li > ol { margin: 0.25em 0 0; }
hr { height: 0; border: 0; border-top: 2px solid var(--border); margin: 1.5rem 0; }
table { border-collapse: collapse; display: block; max-width: 100%; overflow-x: auto; }
th, td { border: 1px solid var(--border); padding: 0.4em 0.8em; }
th { background: var(--code-bg); font-weight: 600; }
img { max-width: 100%; }
del { color: var(--muted); }
`
