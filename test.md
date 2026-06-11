# Demark Test Document

A paragraph with **bold**, *italic*, ***both***, `inline code`,
~~strikethrough~~, and snake_case_words left alone. Here is a
[link](https://example.com "with a title") and an autolink
<https://example.org/path?a=1&b=2> plus an email <someone@example.com>.

Escapes: \*not italic\*, \`not code\`, and a literal backslash \\ char.
HTML chars get escaped: 5 < 6 & 7 > 2, "quoted".
Entities pass through: &copy; &amp; &#169;.

Hard break here:  
second line after break.

## Lists

- unordered one with *emphasis*
- two
  - nested a
  - nested b
    1. deep ordered
    2. deeper still
- three with `code`

1. first
2. second

   A loose second paragraph inside item two.

3. third

5) ordered starting at five
6) six

## Block Elements

> A blockquote with **bold** text,
> lazy continuation on this line.
>
> > And a nested quote.

```go
func main() {
	fmt.Println("hello <world> & friends")
}
```

    indented code block
    second line

Setext Heading
==============

Another Setext
--------------

---

## Table

| Name    | Qty | Price |
|:--------|:---:|------:|
| Apple   |  3  | $1.25 |
| Banana  | 12  | $0.35 |
| Mango \| etc | 1 | $2.00 |

## Media & HTML

![A tiny image](https://example.com/img.png "img title")

<div class="custom">
Raw <em>HTML block</em> passes through.
</div>

Inline <kbd>Ctrl</kbd>+<kbd>C</kbd> tags work too. Code span with
backtick: `` ` `` and `a *b* c` stays literal inside code.

### Duplicate Heading
### Duplicate Heading
