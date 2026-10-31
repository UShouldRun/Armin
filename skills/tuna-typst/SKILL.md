# Typst documents in the "Tuna" house style — Henrique's richest
# Typst form. The plain typst skill covers CVs and letters; this one
# covers the *designed document*: formal reports, riders, project
# roadmaps. Everything below is extracted from
# ~/Documents/tuna/sida.typ (the master example) and rider.typ.

## Workflow
1. Read an existing .typ in this style before writing a new one —
   reuse the helpers instead of inventing new ones.
2. Write the document with the structure below.
3. `typst compile file.typ` must exit clean. A snap-installed typst
   can't read hidden dirs or /tmp; keep files in normal folders.

## Document skeleton (in order)

1. `#set document(title: ..., author: ...)` first of all.
2. `#set page(...)` with a 3-column header grid (org name left, an
   image or blank center, date right) over a rule; a footer grid with
   the document name left and
   `Página #context counter(page).display("1 de 1", both: true)`
   right, over a rule; margins `(top: 2.5cm, bottom: 2.5cm,
   left: 2.8cm, right: 2.8cm)`.
3. A `background:` watermark, centered:
   `place(center + horizon, image("emblema.png", width: 80%, fit: "contain"))`.
4. `#set text(font: "Libertinus Serif", size: 10.5pt)`, `lang: "pt"`.
5. `#set par(justify: true, leading: 0.75em, spacing: 1.2em)`.
6. Heading restyling per level (1: `v(1.2em)` before, `v(0.5em)`
   after; 2: 0.8em/0.3em; 3: 0.5em/0.2em), `#set
   heading(numbering: "1.1.")`.
7. A colour palette as `#let` constants (see below).
8. Helper components (`#let`, see below).
9. Sections separated by comment banners:
   `// ══...══  N. TITLE  ══...══`
10. `#pagebreak()` between major sections.
11. A cover page: `#page(header: none, footer: none)[...]` with
    `#align(center)[...]`, big type, `#line(length: 70%, stroke:
    1.5pt)` rules above and below, `#v(1fr)` pushing the org block
    to the bottom.
12. After the cover: `#outline(title: [Índice], depth: 2, indent:
    1.5em)` then `#pagebreak()`.
13. Internal cross-references: headings get labels
    (`= Section <label>`) and body text links with
    `#link(<label>)[text]`.

## The palette (hex constants at the top)

```
#let primary   = rgb("#1a3a5c")   // dark navy: big titles
#let secondary = rgb("#2e6da4")   // table headers, step numbers
#let accent    = rgb("#e8f0f8")   // info panels
#let warn      = rgb("#fff3cd")   // note boxes
#let warn-border = rgb("#d4a017")
#let green-bg     = rgb("#e6f4ea")
#let green-border = rgb("#2e7d32")
#let red-bg       = rgb("#fdecea")
#let red-border    = rgb("#c62828")
```

## Helper components (copy these, don't reinvent)

- `note-box(body, bg: warn, border: warn-border, label: "Nota")` —
  a callout with a 3pt left stroke:
  `block(width: 100%, fill: bg, stroke: (left: 3pt + border),
  inset: 10pt/8pt, radius: 2pt)` with a bold colored label line.
  Variants: `label: "Importante"`, `label: "Atenção"` with
  `bg: red-bg, border: red-border`.
- `step-block(num, title, body)` — a numbered process step:
  `block(fill: accent, stroke: (left: 3pt + secondary))` holding a
  2-column grid: a 22pt circle box (`radius: 11pt`, `fill:
  secondary`) with the white number, and bold `primary` title + body.
- `decision(condition, yes, no)` — a decision diamond: an italic
  `⬦ Decisão: condition` header block, then a 2-column grid of a
  green `✓ Sim` panel and a red `✗ Não` panel, each
  `block(fill: green-bg, stroke: (left: 3pt + green-border),
  inset: 8pt, radius: 2pt)`.
- `legend-item(color, label)` — a swatch + label 2-column grid.

## Tables (the house table)

```
#table(
  columns: (...),
  align: center + horizon,          // or a per-column function
  stroke: 0.5pt + rgb("#d1d5db"),
  fill: (_, row) => if row == 0 { secondary }
    else if calc.odd(row) { white } else { rgb("#f3f4f6") },
  inset: (x: 10pt, y: 7pt),
  table.cell[#text(fill: white, weight: "bold", size: 9pt)[Header]], ...
)
```

Dark `secondary` header row with white bold text, alternating
white/light-grey body rows, thin grey strokes, generous inset.

## Figures

`#figure(image("file.png", width: 100%), caption: [...])` —
numbered, referenceable from text ("na Figura 1"). For diagrams
without an image file, draw with native Typst shapes (`rect`,
`line`, `circle` inside a `ceti` block) or build the "figure" from
grids and boxes.

## Writing conventions

- Prose is Portuguese (pt-PT) in the Tuna documents; English in
  technical ones. Match the existing document's language.
- Section titles sentence-cased and short; the comment banner
  above each carries the numbering.
- Em-dash `---` in Typst source renders as "—".
- `\\` forces a line break where a grid cell needs it.
- No filler: each section opens with its purpose in one sentence,
  then the structured content.

## Pitfalls (learned the hard way on paper.typ)

- Never put command names or multi-letter words in math mode
  (`$ls$`, `$callers$` are *unknown variables*). Use `code` or
  quotes: `` `ls` ``, `$O("cond")$` not `$O(cond)$`.
- `#` in math mode is a function call, never use as a sigil.
- `|word|` is absolute-value math, still an unknown variable —
  use quoted strings inside `$...$`.
- A `snap` typst can't read `~/.config`, `~/.cache` or `/tmp`.