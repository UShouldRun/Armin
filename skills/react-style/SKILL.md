---
name: react-style
description: The user's React/JSX and HTML style — spaced "=" on attributes, aligned attribute columns, padded { expr }, airy layout. Load before writing or editing JSX, TSX or HTML.
---
Markup (JSX, TSX and plain HTML) follows one spaced style. Components
and hooks follow the js-style skill as well — load it too for the
non-markup parts.

## Attributes
- Spaces around `=`: `className = "card"`, `onClick = { open }`.
  Same in HTML: `<html lang = "pt">`.
- One attribute per line once an element has several, with the `=`
  aligned by padding the names:

  ```jsx
  <button
    key       = { pillar.key }
    className = "nav-item"
    onClick   = { () => select(pillar.key) }
  >
  ```

## Expressions
- Expression containers are padded: `{ value }`, `{ a } de { b }`,
  `{ ...props }`, and `${ x }` in template literals.
- Object-literal props keep the double braces tight:
  `style = {{ marginTop: "1rem" }}`.
- Multi-line expressions open `{` (or `= {`) at the end of the line,
  indent the body, and close `}` on its own line:

  ```jsx
  { items.length > 0 && (
    <ul>
      { items.map(item => (
        <li key = { item.id }>{ item.name }</li>
      )) }
    </ul>
  ) }
  ```
- `{/* comments */}` stay unpadded.

## Layout
- 2-space indentation.
- Blank lines between sibling blocks; spaced-out rather than dense.
- Arrow-function components, `export default` at the end of the file.

## Blank lines between blocks
- Blank lines between sibling blocks of markup.
- In component code, the js-style rule: a blank line after each
  closing `}` when code follows, and before a `return` that follows
  a group of statements.

## Before finishing
Re-read the diff for missing blank lines between blocks,: `attr=x` without spaces, unaligned `=` in
multi-attribute elements, unpadded `{x}` / `${x}`.
