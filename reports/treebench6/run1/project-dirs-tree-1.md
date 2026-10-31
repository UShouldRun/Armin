How does armin save chat sessions to disk?

right false, 122890 prompt (103680 cached), 1594 completion, $0.01716, 10 rounds, 13 tools

## answer
Done — covered above.

## calls
know_find map[query:chat session persistence storage save disk]
semantic_search map[query:saving chat sessions to disk persistence]
search_code map[depth:2 file_typego:</function>
</tool_call>
<tool_call>
<function=list_directory>
<parameter=path>
internal pattern:session]
list_directory map[path:internal]
search_code map[file_typego:</function>
</tool_call>
<tool_call>
<function=find_files>
<parameter=pattern>
*session* pattern:session]
read_file map[path:internal/session/session.go]
read_file map[path:internal/session/session.type.go]
read_file map[end_line:220 path:internal/session/session.go start_line:1]
find_references map[name:session.Append]
find_references map[name:session.Save]
read_file map[end_line:421 path:internal/repl/repl.go start_line:385]
know_write map[claim:Each chat session is one JSONL file &lt;uuid&gt;.jsonl under &lt;XDG_DATA_HOME|~/.local/share&gt;/armin/sessions: a Meta line (id,…
know_write map[claim:Writing: session.Save truncates and rewrites the whole file (infrequent: /save, rename, exit flush via REPL SaveNow). session.Appe…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473424974417517"
author a2 armin session "bench-1793473424974417517"

n1 persistence topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 persistence/sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 persistence/sessions/storage fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Each chat session is one JSONL file &lt;uuid&gt;.jsonl under &lt;XDG_DATA_HOME|~/.local/share&gt;/armin/sessions: a Meta line (id, root, model, title, timestamps) then one JSON line per llm.Message. Dir 0700, file 0600."
  evidence file "internal/session/session.go:36-198" d2686290
  evidence file "internal/session/session.type.go:1-28" 336538f9

n4 persistence/sessions/writing fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Writing: session.Save truncates and rewrites the whole file (infrequent: /save, rename, exit flush via REPL SaveNow). session.Append writes only history[*persisted:], advancing *persisted — the per-turn autosave path (REPL maybeAutosave, web UI), O(1) amortized. /clear and /compact must Save and reset persisted."
  derived-from n3
  evidence file "internal/session/session.go:140-247" 7edf4d7d
  evidence file "internal/repl/repl.go:397-420" 94b69314
  evidence session "internal/repl/sessions.go:44,65" 00000000
  evidence file "internal/web/web.go:111" e97f1d25

n5 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n6 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/repl/repl.go:385-421 internal/session/session.go internal/session/session.go:1-220 internal/session/session.type.go"
  evidence file "internal/repl/repl.go:385-421" 27a14fe0
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence session "internal/session/session.type.go" 00000000

