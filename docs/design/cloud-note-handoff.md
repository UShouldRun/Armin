# Handoff to Claude Opus 5.5 — since your last active round

You last saw the repo after round 9's consolidation work (commits
5c05413 + 23c1adb, the write-up you helped with in
docs/design/cloud-note-round7.md and the round 9 section now in
docs/design/restructure-tree-economics.md). Since then the session
here was almost entirely UI: the REPL banner/header and the web UI.
Everything below is **uncommitted** on `restructure`, verified by
`go build ./...` + the package tests noted, and (mostly) by the user
rebuilding `armin-test` via `./install-test.sh` and eyeballing it.

## REPL header (internal/ui, internal/repl, internal/display)

1. **Logo actually renders now — two bugs fixed.**
   a. Kitty chunking: continuation chunks were emitted as bare
      `,m=0;<base64>` without their own `\x1b_G` opener — the base64
      dumped as literal text. Every chunk now carries a full escape.
   b. Worse: the `m` flag semantics were inverted (`m=1` on the first
      chunk, `m=0` on continuations). Per the spec `m=1` = "more
      follows", `m=0` = **last chunk** — the terminal aborted at
      chunk 2. Now: `m=1` on all but the last, `m=0` on it.
      (`TestLogoKitty` in ui_test.go updated to the correct contract.)
   c. `assets/logo_dark.png` resized 1264×848/35 KB → 144×50/3070 B so
      the base64 (4094 chars) fits a single 4096-byte chunk —
      `LogoPath`'s comment now documents that constraint.
      `LogoCols`/`LogoRows` 20/7 → 18/3 to match.
   d. `Logo()` also skips itself under `$TMUX` (tmux mangles APC).
2. **Header layout per user spec** (ui.go `bannerText`, repl.go ~222):
   tagline + `root:` + blank line, then the Armin instance line,
   context line, bold session title. The old `model:` and
   "type /help" lines are gone. `TestBanner` updated accordingly.
3. **Startup notice under the header, finally.** `refreshKnow`
   queues the "knowledge DAG: N stale …" summary inside
   `think.New`, before any frontend exists; the old single
   `go printNotices` in `display.Run` raced the banner and usually
   won, printing to stderr *above* it. Now each mode starts its own
   drain: non-REPL modes keep the stderr drain in `display.Run`'s
   switch; REPL mode stores the REPL (`notices.Store`) and *then*
   starts the drain in `runREPL`, so the notice lands at the first
   prompt. `REPL.Notice` (host.go) prints it indented like the
   banner's status lines; the prompt's own leading `\n` separates.
4. `Chore()` (internal/llm/client.go) now sends `Effort: "low"`
   instead of `"none"` — OpenRouter's GLM 400s with "Reasoning is
   mandatory for this endpoint", which was breaking `Compact()`.
   `TestChoreTurnsOffHostedReasoning` asserts `low` now.

## Web UI (internal/web/static — all `go:embed`ed, needs rebuild)

5. **File tree fixed and restyled**: the collapse feature crashed the
   whole render (`expanded` set referenced but never defined —
   nothing rendered at all). Now dirs start collapsed, dirs sort
   before files, toggle state survives re-renders (viewer.js).
6. **Composer**: header buttons (compact / clear / approve all /
   viewer) moved into a `↑` dropup menu next to Send (opens upward,
   centered, bold centered entries, `↑` for `⋯` — chat.css, index.html,
   main.js wiring with outside-click/Escape close).
7. **Context readout** moved from the header's meter bar into the
   composer, left of the input, formatted like the REPL's gauge
   (`49k/98k 50%`), colored dim → yellow ≥50% → red ≥75%
   (header.js `showContext`; callers in header.js + chat.js SSE
   `usage` event; the tier parameter is gone).
8. **Root path bar**: new `.path[data-root]` div above the file
   filter (filled from `/api/info`'s `root`), scrolls horizontally;
   file rows scroll horizontally instead of ellipsizing. The
   header's copy was removed; `header select { margin-left: auto }`
   restores the old right-aligned controls (logo alone left).
9. **Markdown images** (from earlier): `![]()` and raw-HTML `<img>`
   rules rewrite relative src → `/api/raw?path=`; small header font;
   mid-run approve-all ordering fix in handlers.go (`approve` before
   `TryLock`).

## Round 9 verdict (already in the tree-economics doc)

Consolidation between sessions: **+10.2%** — didn't pay. Confounds
noted there (run 3 lucky-plain, claims arriving too late for s3);
next-round proposal: make the s2→s3 pass the thing s3 actually sees.

## Loose ends you might care about

- All of the above is uncommitted on `restructure`.
- `install-test.sh` rebuilds `armin-test` into `~/.local/bin` — the
  web static assets are embedded, so every static change needs it.
- `write_file` corrupts >~500 B content in this environment; use
  `edit_file` on placeholder seeds and verify with `file_stat`.
- No JS test harness exists for `internal/web/static` — verified by
  build + manual browser checks after rebuild.
