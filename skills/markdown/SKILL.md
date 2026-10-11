---
name: markdown
description: Write and edit Markdown (READMEs, docs, notes, changelogs) in the user's conventions — prose wrapped at 80 columns or less if asked, tables exempt but column-aligned, fenced code with a language, one blank line between blocks. Load before writing or reformatting any .md file.
---
## Line width
- Wrap every prose line at **80 characters or fewer**. If the user
  asks for a narrower width (e.g. 72 or 70), use theirs for the whole
  file.
- Break at word boundaries; never split a word, a URL or an inline
  `code span` across lines. A single token longer than the limit
  (a long URL) goes on its own line and may run over.
- List items wrap with a hanging indent aligned to the item's text:

  ```markdown
  - A list item that runs past the limit continues on the next line,
    indented to line up with the text above.
  ```
- Headings stay on one line; shorten the heading rather than wrap it.
- **Tables are exempt** from the limit: a row is one line, however
  long. Code blocks keep their own formatting (follow the language's
  style, not this limit).
- When editing an existing file, keep its wrapping. Re-wrap only the
  paragraphs you change, so diffs stay small.

## Tables
- Align the columns: pad cells with spaces so every `|` lines up, and
  size the delimiter row to the column width.

  ```markdown
  | Flag       | Default | Meaning                       |
  |------------|---------|-------------------------------|
  | `-model`   | local   | which configured model to use |
  | `-add-dir` |         | extra read-only directory     |
  ```
- Alignment colons (`:---`, `---:`) only where they help: numbers
  right-aligned.
- Keep cells short; move explanations into prose under the table.

## Structure
- One `#` title per file, then `##` sections; don't skip levels.
- One blank line between blocks: before and after headings, lists,
  code fences, tables and block quotes. Never two blank lines.
- `-` for bullets, `1.` for ordered steps (numbered 1, 2, 3 — not all
  1s). Nested lists indent two spaces under the text.
- Fenced code blocks with a language tag (` ```go `, ` ```sh `,
  ` ```text ` for plain output). No indented code blocks.
- Shell examples show the command only, no `$ ` prompt, unless output
  follows it.
- Inline code for file names, commands, flags, identifiers and
  values: `go test ./...`, `-add-dir`, `config.toml`.
- Links: inline `[text](url)` for one-off links; reference-style
  `[text][ref]` with definitions at the end of the section when the
  same URL repeats or the inline form would wreck the wrapping.
- Emphasis sparingly: **bold** for the one thing a reader must not
  miss, *italics* for terms. No ALL CAPS.
- No trailing whitespace, no hard breaks with two trailing spaces;
  end the file with one newline.

## Writing
- Lead with what the reader needs: what it is, how to install/run
  it, then details.
- Short paragraphs, active voice, concrete examples over
  description.
- Write file paths and commands exactly as they are typed.

## Checking
- Find lines over the limit (tables and code fences excepted) with
  read_file and count, or run_command:
  `{"binary":"grep","args":["-nE",".{81,}","README.md"]}` — then
  ignore matches inside tables (`|`-lines) and code fences.
