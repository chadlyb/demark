# demark

A self-contained Markdown to HTML converter in a single Go file, using only
the standard library. Output is a standalone HTML document with a baked-in
GitHub-flavored stylesheet, including automatic dark mode — no external CSS,
scripts, or dependencies.

## Install

```
go install github.com/chadlyb/demark@latest
```

Or build from a checkout:

```
go build -o demark .
```

## Usage

```
demark foobar.md              # writes foobar.html
demark foobar.md -o out.html  # explicit output file
demark foobar.md -o -         # write to stdout
demark -                      # read stdin, write stdout
```

The document `<title>` is taken from the first `# heading`, falling back to
the input filename. demark refuses to overwrite its own input file.

## Supported Markdown

- ATX (`#`…`######`) and setext headings, with auto-generated anchor ids
  (deduplicated for repeated titles)
- Paragraphs, hard line breaks (two trailing spaces or trailing `\`)
- Emphasis: `*em*`, `**strong**`, `***both***`, with `_underscore_` variants
  (intraword underscores like `snake_case` are left alone)
- GFM `~~strikethrough~~`
- Inline code, fenced code blocks (with `language-*` class), indented code
  blocks
- Links with titles, images, autolinks (`<https://…>` and email addresses),
  backslash escapes
- Nested ordered and unordered lists, including non-1 start numbers and
  loose/tight rendering
- Blockquotes with nesting and lazy continuation
- GFM tables with per-column alignment and escaped pipes
- Horizontal rules, HTML entities, raw inline and block HTML passthrough

Not supported, by design: reference-style links (`[text][ref]`), entity
decoding (entities pass through verbatim), and the more exotic corners of
CommonMark's emphasis delimiter algorithm.

## Testing

```
go test ./...
```

The suite includes:

- Golden unit tests for demark's own behavior and extensions
- The official [CommonMark 0.31.2 spec suite](https://spec.commonmark.org/)
  (652 examples, vendored in `testdata/spec.json`) — 431 pass; the exact
  passing set is pinned in `testdata/commonmark_pass.txt`, and the test
  fails if any pinned example regresses
- The official GFM table and strikethrough extension examples — 10/10 pass
- A fuzz target (`go test -fuzz=FuzzMarkdownBody`); previously found
  crashers are kept in `testdata/fuzz/` as regression cases

After an intentional behavior improvement, regenerate the spec pass lists
with `go test -update`.
