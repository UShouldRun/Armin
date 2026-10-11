---
name: typst
description: Write and fix Typst documents (CVs, cover letters, reports, papers) in the user's conventions — set-rule preamble, small #let helpers with named none defaults, dense one-page CVs, justified letters — and compile to check. Load before touching any .typ file.
---
## Workflow
1. Read the existing .typ first; reuse its helpers (`section`, `entry`,
   ...) instead of inventing new ones.
2. Make the change.
3. Compile: `typst compile file.typ` (or `typst compile file.typ
   out.pdf`). It has to compile with no errors. If `typst` isn't
   allowlisted for run_command, ask the user to add `allow = typst`
   or to set `build_cmd = typst compile <file>.typ`, and then use
   build_check.
4. For a CV, check it still fits its page budget (usually one page):
   count pages with `pdfinfo out.pdf | grep Pages` if available, or
   ask the user to look.
5. A snap-installed typst can't read hidden directories (`~/.config`,
   `~/.cache`) or `/tmp`: "failed to load file (access denied)" means
   the file must live in a normal folder under home.

## The user's conventions
- Preamble of set rules first, one per line:

  ```typst
  #set page(paper: "a4", margin: (x: 30pt, y: 20pt))
  #set text(size: 7.75pt)
  #set par(justify: true, leading: 0.45em)
  ```
- Letters and essays: `font: "New Computer Modern"`, 11pt, justified,
  `leading: 0.65em`, generous margins (72pt or 2cm × 2.5cm). Sender
  block `#align(right)[...]` with `\` line breaks; `#v(1em)` between
  blocks.
- Reusable pieces are `#let` functions. Optional parts are named
  arguments defaulting to `none` and checked with `!= none`:

  ```typst
  #let entry(
    title: none,
    company: none,
    date: none,
    location: none,
    description: none,
  ) = {
    if title != none { text(weight: "bold", title) }
    ...
  }
  ```
  One named argument per line with a trailing comma, both in the
  definition and in calls.
- A section is a bold uppercase title, a thin rule and its content:
  `text(size: 10pt, weight: "bold", upper(title))`,
  `line(length: 100%, stroke: 0.5pt)`, small negative `v(...)` to
  tighten.
- Right-aligned dates in a line with `h(1fr)`; separators as `[ | ]`.
- Two-column blocks with `#grid(columns: (1fr, 1fr), column-gutter:
  16pt, align: top, [...], [...])`.
- Links always carry readable text: `#link("https://...")[label]`.
- Bullets inside `description: [ ... ]` content blocks, wrapped near
  80 columns in the source.
- Disabled content is kept in `/* ... */` rather than deleted.
- Templates to start from: `templates/cv.typ`, `templates/letter.typ`
  (use_skill with file).

## Typst pitfalls
- `@` starts a reference: write emails as `name\@example.com`.
- `#` switches to code mode inside markup; in code blocks (`{ }`)
  markup needs `[ ... ]`.
- `$...$` is math: `$tilde 0.25$`, `$times$`. Use the Unicode `×`, `–`,
  `→` directly in text instead when there's no math.
- `*bold*`, `_italic_`; a literal `*`, `_`, `$`, `#` or `<` needs `\`.
- `\` at the end of a line is a forced line break; a blank line is a
  new paragraph.
- `set` rules apply to everything after them in their scope; to style
  one element, pass arguments to it instead.
- Content arguments can trail the call: `#section[TITLE][body]`
  passes two content arguments.
- `datetime.today().display("[day] [month repr:long] [year]")` for
  dated letters.

## Writing CVs and letters
- CV bullets: what you did, how, and a measured result
  ("~33% edge-cut reduction", "98% vs. 15%"). Past tense for finished
  work, present for ongoing. No filler adjectives.
- Keep the CV to its page budget: tighten wording before shrinking
  font or spacing.
- Cover letters: address the recipient by name, say what you're
  applying for in the first sentence, then two or three paragraphs of
  concrete evidence linked to the role, then a short close. Match the
  letter's language (English or Portuguese) to the posting.
