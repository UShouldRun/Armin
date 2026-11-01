# Style rules

What `style/` checks across the repository. Each `###` section is one
rule: the heading names it, the paragraph says what it wants and why,
and the list under it says how to find a break. Add or change a rule
here; the checker needs no change.

Run from the repository root:

```
go -C style run . ..
```

It prints `file:line: rule` for every break and a count per rule, and
exits 1 when anything broke.

## How a rule is written

A line breaks a rule when it passes every field the rule has:

- `files`: comma-separated globs on the file name (`*.go`, `*.js`).
- `match`: a regular expression (Go's RE2) the line matches.
- `on`: what `match` and `unclosed` see. `code` (the default) is the
  line with the insides of strings, regexes and comments blanked
  out, so a rule about code doesn't fire on text; `line` is the line
  as written.
- `except`: a regular expression the line doesn't match.
- `next` / `next-not`: a regular expression the next non-empty line
  matches / doesn't match.
- `next-blank: no` / `prev-blank: no`: the line right after / right
  before isn't blank.
- `same-indent: yes`: the next non-empty line starts at the same
  column.
- `opener`: for a line that starts by closing a bracket, a regular
  expression the line that opened it matches.
- `unclosed`: the line leaves at least this many brackets open
  (`(`, `[` and `{` it opens minus those it closes).
- `skip-files`: comma-separated globs of files the rule leaves alone.

Values are written in backquotes; one that contains a backquote is
written in double backquotes with a space inside them: ``` `` a`b `` ```.

Lines inside a multi-line raw string (a Go backquoted string, a JS
template literal) are text, not code: no rule looks at them.

## Go

### tab indent
Indent with two spaces, never tabs: the code is deliberately not
gofmt-formatted.
- files: `*.go`
- on: line
- match: `^\t`

### over 80 columns
Lines around 70 columns, 80 at most. A URL can't be broken, and a
model prompt's first line is prompt text: wrapping it would change
what the model is sent.
- files: `*.go`
- on: line
- match: `^.{81,}`
- except: `` https?://|^\s*(const|var)?\s*\w+\s*=\s*` ``

### declaration in an if header
Never `if x := f(); x != nil {`: the statement goes on its own line
above the `if`, which only tests.
- files: `*.go`
- match: `^\s*(\}\s*else\s+)?if\s[^{]*;`

### one-line block
Braces are not optional and single-line bodies are not a thing: an
`if`, `for` or `switch` body is never on the line that opens it.
- files: `*.go`
- match: `^\s*(\}\s*else\s+)?(if|for|switch)\b.*\{\s*\S.*\}\s*$`

### unspaced comparison or logic
A space on both sides of every binary operator: `a == b`,
`x != nil`, `a && b`, `x := y`, `i += 1`.
- files: `*.go`
- match: `[\w)\]](==|!=|<=|>=|&&|\|\||:=|\+=|-=|\*=|/=|<<|>>)[\w(\[!&*-]`

### unspaced arithmetic
`len(h) - 1`, `s[:i + 1]`, `64 * 1024`, `a + b`, `x = y`: never
`i+1`, `a*b` or `x=y`.
- files: `*.go`
- match: `[\w)\]]([+/%^|]|-[^-=>]|&[^&^=])[\w(\[]|[\w)]\*[\w(\[]|[\w)\]]=[\w(\[]`

### tight braces
A one-line literal pads its braces: `{ x }`, `[]string{ "a" }`,
`T{ A: 1 }`. Empty `{}` stays tight.
- files: `*.go`
- match: `[\w\]]\{[^\s}]|[^\s{]\}[^\s]?$|[^\s{]\}[,)]`

### bracket hugging
A call or literal either fits on one line, or puts each argument on
its own line (short plain ones may share one) with the closer on its
own line. Never open a second bracket on the line that opened the
call, as in `f(a, T{` or `t.Run(name, func() {`.
- files: `*.go`
- unclosed: 2

### hugged closer
The other half of the rule above: a line never starts by closing two
brackets at once, as in `})` or `}}`.
- files: `*.go`
- match: `^\s*[}\])]\s*[)\]}]`

### blank line after a block
A blank line after the closing `}` of an `if`, `for`, `switch` or
function when more code follows at that level (not before `else`,
`case`, `default` or another closer).
- files: `*.go`
- match: `^\s*\}$`
- opener: `\b(if|for|switch|select|else|func)\b`
- next-blank: no
- next-not: `^\s*([})\]]|else\b|case\b|default\b|//)`
- same-indent: yes

### error not wrapped
Every returned error is wrapped with the function it passed through:
`fmt.Errorf("\n\t... in Load:\n\t%w", err)`. A message a model reads
as a tool result (errResult) is a plain sentence instead.
- files: `*.go`
- on: line
- match: `fmt\.Errorf\("[^"]*%w`
- except: `\\n\\t\.\.\. in |errResult\(`

## JavaScript

### js tab indent
Two spaces, never tabs.
- files: `*.js`
- on: line
- match: `^\t`

### js over 80 columns
The same 80 columns as Go. A long template that builds HTML is
broken into concatenated pieces, or built in steps.
- files: `*.js`
- on: line
- match: `^.{81,}`
- except: `https?://`

### single quotes
Strings use double quotes (or backquotes when they interpolate).
- files: `*.js`
- on: line
- match: `^[^"/]*'[^']*'`

### unpadded interpolation
Template interpolations are padded: `${ value }`, never `${value}`.
- files: `*.js`
- on: line
- match: `\$\{[^ ]|\$\{ [^}]*[^ ]\}`

### js one-line body
Braces are not optional: never `if (x) return;` or
`if (x) { y(); }` on one line. The body goes on its own lines.
- files: `*.js`
- match: `^\s*(\}\s*else\s+)?(if|for|while)\s*\(.*\)\s*[^\s{].*;\s*$|^\s*(\}\s*else\s+)?(if|for|while)\b.*\{\s*\S.*\}\s*$`

### missing semicolon
Every statement ends with a semicolon.
- files: `*.js`
- match: `` [\w)\]"'`]\s*$ ``
- except: `^\s*(//|\*|import\b.*\{$|export\s*\{$|case\b|default\b|else\b|\}\s*else\b)`
- next-not: `` ^\s*([.?:)\]`]|\+|-|\*|/|&&|\|\||=>) ``

### js unspaced operator
A space on both sides of every binary operator: `a + b`,
`x === y`, `count = count + 1`.
- files: `*.js`
- match: `` [\w)\]](===|!==|==|!=|<=|>=|&&|\|\||\+=|-=|=)[\w(\["'`]|[\w)\]][+*/%][\w(] ``

### js bracket hugging
As in Go: a call opens no second bracket on its own first line,
except a final arrow function or object a reader expects there.
- files: `*.js`
- unclosed: 3

### js blank line after a block
A blank line after a closing `}` when more code follows at that
level (not before `else`, `catch`, `finally` or another closer).
- files: `*.js`
- match: `^\s*\}[;)]*$`
- opener: `\b(if|for|while|switch|else|try|catch|finally|function)\b|=>\s*\{`
- next-blank: no
- next-not: `^\s*([})\].]|else\b|catch\b|finally\b|case\b|default\b|//)`
- same-indent: yes

### js blank line before else
`else`, `catch` and `finally` get their blank line inside, before
the brace that closes the branch above them:

```js
if (busy) {
  hold(text);

} else {
  send(text);
}
```
- files: `*.js`
- match: `^\s*\}\s*(else|catch|finally)\b`
- prev-blank: no

## Not checked here

Column alignment (struct fields, `const` blocks, keyed literals, the
`=` of consecutive JS declarations, the `from` of imports), the blank
line before a `return` that ends a group, and types living in
`X.type.go` need more than a line to see. They are in
skills/go-style and skills/js-style, and reviewed by hand.
