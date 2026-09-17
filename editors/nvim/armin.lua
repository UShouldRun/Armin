-- Minimal Neovim integration for armin's LSP mode.
--
-- Nothing in this file runs on its own: loading it (require("armin"))
-- does nothing, and setup() only registers user commands — it does
-- not start armin, attach anything, or turn completion on. The armin
-- process only starts when you run :ArminLsp yourself, and stops when
-- you run :ArminStop. Ghost-text completion is the one thing here
-- that runs on a timer once turned on — and it only turns on when you
-- run :ArminComplete; nothing fires before that. No autocmds set up
-- front, no keymaps claimed for you — wire those up yourself if you
-- want them (see docs/nvim.md).
--
-- Requires Neovim >= 0.10 (vim.lsp.get_clients, client.request).

local M = {}

local client_name = "armin"
local ns = vim.api.nvim_create_namespace("armin_completion")

-- bufnr -> { id, text, row, col }: the ghost-text suggestion currently
-- shown in that buffer, if any.
local suggestion = {}
-- bufnr -> true while auto ghost-text (:ArminComplete) is on for it.
local auto_enabled = {}
-- bufnr -> augroup id backing that buffer's auto mode, so toggling
-- off tears it down cleanly instead of leaving stray autocmds.
local augroups = {}
-- bufnr -> generation counter. Debouncing without juggling timer
-- handles: a scheduled request only fires if it's still the latest
-- one scheduled for that buffer.
local gens = {}

local function find_root(start)
  local markers = { ".git", "go.mod", "package.json", "pyproject.toml", "Cargo.toml" }
  local dir = vim.fs.dirname(start)
  local found = vim.fs.find(markers, { path = dir, upward = true })[1]
  if found then
    return vim.fs.dirname(found)
  end
  return vim.fn.getcwd()
end

local function get_client()
  return vim.lsp.get_clients({ name = client_name })[1]
end

-- wrap_text hard-wraps each paragraph of text to width columns,
-- preserving existing blank-line paragraph breaks. armin's answers
-- come back as prose with no line breaks of their own; without this
-- they render as one unbroken wall of text in the floating window.
local function wrap_text(text, width)
  local out = {}
  for _, para in ipairs(vim.split(text, "\n", { plain = true })) do
    if para == "" then
      out[#out + 1] = ""
    else
      local line = ""
      for word in para:gmatch("%S+") do
        if line == "" then
          line = word
        elseif #line + 1 + #word <= width then
          line = line .. " " .. word
        else
          out[#out + 1] = line
          line = word
        end
      end
      if line ~= "" then
        out[#out + 1] = line
      end
    end
  end
  return out
end

-- list_project_files shells out to rg (already required — see
-- search_code) rather than a slower Lua directory walk or adding a
-- new dependency just for this.
local function list_project_files(root)
  local ok, out = pcall(vim.fn.systemlist, { "rg", "--files", root })
  if not ok or vim.v.shell_error ~= 0 then
    return {}
  end
  local rel = {}
  for _, f in ipairs(out) do
    rel[#rel + 1] = f:sub(#root + 2) -- strip the "root/" prefix
  end
  return rel
end

-- coding_keywords mark an installed model as coding-focused, for
-- the fastest/best recommendation tags in :ArminModel's picker.
local coding_keywords = {
  "coder", "code", "codestral", "starcoder", "codegemma", "deepseek-coder",
}

local function is_coding_model(name)
  local lower = name:lower()
  for _, kw in ipairs(coding_keywords) do
    if lower:find(kw, 1, true) then
      return true
    end
  end
  return false
end

local function human_size(bytes)
  if not bytes or bytes <= 0 then
    return ""
  end
  return string.format("%.1fGB", bytes / (1024 * 1024 * 1024))
end

-- tag_models returns a name -> tag map for the coding-focused
-- models among installed ({name, size} entries): the smallest is
-- tagged "fastest", the largest "best" — quality tends to track
-- size within the same model family, a heuristic, not a benchmark.
-- Non-coding models and ties get no tag.
local function tag_models(installed)
  local coding = {}
  for _, m in ipairs(installed) do
    if is_coding_model(m.name) and m.size and m.size > 0 then
      coding[#coding + 1] = m
    end
  end
  table.sort(coding, function(a, b) return a.size < b.size end)

  local fastest, best = coding[1], coding[#coding]
  local tags = {}
  if fastest and best and fastest ~= best then
    tags[fastest.name] = "fastest"
    tags[best.name] = "best"
  elseif fastest then
    tags[fastest.name] = "only coding model installed"
  end
  return tags
end

-- extract_mentions finds "@relative/path" references in a question
-- — the file-mention feature just inserts these as plain text, and
-- armin's agent already has its own read_file/search_code tools to
-- act on them; this only extracts them to nudge the model to look.
local function extract_mentions(text)
  local mentions = {}
  for path in text:gmatch("@([%w%._%-/]+)") do
    mentions[#mentions + 1] = path
  end
  return mentions
end

-- Start (or reattach) the armin LSP client for the current buffer.
-- Root is the nearest .git/go.mod/package.json/... ancestor, falling
-- back to the current working directory.
function M.start()
  if vim.fn.executable("armin") ~= 1 then
    vim.notify("armin: binary not found on PATH", vim.log.levels.ERROR)
    return
  end

  local root = find_root(vim.api.nvim_buf_get_name(0))

  local id = vim.lsp.start({
    name = client_name,
    cmd = { "armin", "-lsp", "-root", root },
    root_dir = root,
    -- armin/askEvent streams an in-flight armin.ask's tool calls and
    -- answer tokens ahead of its final response — see M._chat_on_*.
    -- Only one ask is ever in flight from this client, so the
    -- notification's id (for routing multiple in-flight asks) is
    -- ignored; every event just targets the one open chat window.
    handlers = {
      ["armin/askEvent"] = function(_, result)
        if not result then
          return
        end
        if result.type == "tool_call" then
          M._chat_on_tool_call(result.name, result.args)
        elseif result.type == "token" then
          M._chat_on_token(result.delta)
        end
      end,
    },
  }, { bufnr = 0 })

  if id then
    vim.notify("armin: attached (" .. root .. ")", vim.log.levels.INFO)
  end
end

-- Stop the armin client entirely, and turn off completion everywhere
-- it was on — a stopped client can't answer those requests anyway.
function M.stop()
  local client = get_client()
  if not client then
    vim.notify("armin: not running", vim.log.levels.WARN)
    return
  end

  for bufnr in pairs(auto_enabled) do
    M._set_auto(bufnr, false)
  end
  vim.lsp.stop_client(client.id)
  vim.notify("armin: stopped", vim.log.levels.INFO)
end

-- responseWidth is the hard-wrap column for the chat window's own
-- width — matches this project's own 70-80 col style rather than
-- however wide the window happens to be.
local responseWidth = 78

-- ref_ns highlights [[path:line]] / [[path:start-end]] citations
-- armin's answer includes when flagging something worth attention
-- (see citationInstruction, lspserver side) — never inserted for
-- routine file mentions, so every one found is worth making
-- jump-to-able.
local ref_ns = vim.api.nvim_create_namespace("armin_chat_refs")

vim.api.nvim_set_hl(0, "ArminRef", { link = "Underlined", default = true })

-- ref_pattern matches the raw citation markup wherever it lands in
-- wrapped output. It's deliberately left in place rather than
-- rewritten to a shorter label: the markup itself has no spaces, so
-- wrap_text's word-splitting always keeps one whole, and that means
-- a match found post-wrap sits at the exact column it renders at —
-- no position-tracking through the wrap needed.
local ref_pattern = "%[%[([^%[%]]+)%]%]"

-- parse_ref turns "path:123" or "path:120-135" into its parts, or
-- nil if inner doesn't look like either shape.
local function parse_ref(inner)
  local path, a, b = inner:match("^(.-):(%d+)%-?(%d*)$")
  if not path or path == "" then
    return nil
  end
  local start_line = tonumber(a)
  return { file = path, start_line = start_line, end_line = tonumber(b) or start_line }
end

-- scan_refs finds every citation in lines (already wrapped, already
-- written to buf starting at buffer line line_offset), highlights
-- each with ArminRef, and returns them with buffer positions for
-- M._chat_open_ref's cursor-position lookup.
local function scan_refs(buf, lines, line_offset)
  local refs = {}
  for i, line in ipairs(lines) do
    local search_from = 1
    while true do
      local s, e, inner = line:find(ref_pattern, search_from)
      if not s then
        break
      end
      local ref = parse_ref(inner)
      if ref then
        ref.row, ref.col_start, ref.col_end = line_offset + i - 1, s - 1, e
        refs[#refs + 1] = ref
        vim.api.nvim_buf_add_highlight(buf, ref_ns, "ArminRef", ref.row, ref.col_start, ref.col_end)
      end
      search_from = e + 1
    end
  end
  return refs
end

local function chat_valid()
  return M._chat ~= nil
    and vim.api.nvim_buf_is_valid(M._chat.buf)
    and vim.api.nvim_win_is_valid(M._chat.win)
end

local function chat_scroll()
  if not chat_valid() then
    return
  end
  local n = vim.api.nvim_buf_line_count(M._chat.buf)
  pcall(vim.api.nvim_win_set_cursor, M._chat.win, { n, 0 })
end

-- chat_insert appends lines just above the buffer's trailing prompt
-- line — the prompt-buffer convention keeps exactly one editable
-- line at the end, so "append to history" always means "insert
-- right before it."
local function chat_insert(lines)
  if not chat_valid() then
    return
  end
  local last = vim.api.nvim_buf_line_count(M._chat.buf) - 1
  vim.api.nvim_buf_set_lines(M._chat.buf, last, last, false, lines)
  vim.bo[M._chat.buf].modified = false
  chat_scroll()
end

-- chat_set_active shows (or updates in place) the one "something is
-- happening right now" line — "is armin still thinking?" answered by
-- always having exactly one ◐-prefixed line visible while busy,
-- rather than the model just going silent between visible events.
local function chat_set_active(text)
  local chat = M._chat
  if not chat_valid() then
    return
  end
  if chat.active_row then
    vim.api.nvim_buf_set_lines(
      chat.buf, chat.active_row, chat.active_row + 1, false, { text }
    )
    vim.bo[chat.buf].modified = false
  else
    chat_insert({ text })
    chat.active_row = vim.api.nvim_buf_line_count(chat.buf) - 2
  end
  chat_scroll()
end

-- chat_finish_active flips the current active line's ◐ to ✓ and
-- stops tracking it — called whenever whatever it announced is now
-- known to be over (another event arrived, or the turn ended).
local function chat_finish_active()
  local chat = M._chat
  if not chat_valid() or not chat.active_row then
    return
  end
  local line = (vim.api.nvim_buf_get_lines(
    chat.buf, chat.active_row, chat.active_row + 1, false
  ))[1] or ""
  vim.api.nvim_buf_set_lines(
    chat.buf, chat.active_row, chat.active_row + 1, false,
    { (line:gsub("◐", "✓", 1)) }
  )
  vim.bo[chat.buf].modified = false
  chat.active_row = nil
end

-- quote_arg and join_args back tool_verb's per-tool labels below —
-- kept small and forgiving (never error on a missing/odd-typed
-- field) since a malformed label would be worse than a generic one.
local function quote_arg(v)
  if v == nil or v == "" then
    return "…"
  end
  return "\"" .. tostring(v) .. "\""
end

local function join_args(args)
  if type(args) ~= "table" then
    return ""
  end
  local parts = {}
  for _, v in ipairs(args) do
    parts[#parts + 1] = tostring(v)
  end
  return table.concat(parts, " ")
end

-- tool_verbs renders a human, present-tense description of a tool
-- call — "Reading sandbox.go", not "read_file(path=sandbox.go)",
-- the same spirit modern coding-agent UIs use instead of dumping raw
-- arguments. Anything not listed here (including any tool added
-- later) still gets a sane "Running <name>" fallback via tool_verb.
local tool_verbs = {
  search_code = function(a) return "Searching for " .. quote_arg(a.pattern) end,
  read_file = function(a) return "Reading " .. tostring(a.path) end,
  list_directory = function(a)
    return "Listing " .. (a.path and tostring(a.path) or "the project")
  end,
  run_command = function(a)
    return "Running `" .. tostring(a.binary) .. " " .. join_args(a.args) .. "`"
  end,
  web_search = function(a) return "Searching the web for " .. quote_arg(a.query) end,
  count_lines = function() return "Counting lines of code" end,
  list_languages = function() return "Breaking the project down by language" end,
  find_files = function(a) return "Finding files matching " .. quote_arg(a.pattern) end,
  file_stat = function(a) return "Checking " .. tostring(a.path) end,
  find_todo_fixme = function() return "Scanning for TODO/FIXME comments" end,
  env_info = function() return "Checking the environment" end,
  project_manifest = function() return "Reading the project layout" end,
  find_definition = function(a) return "Finding where " .. quote_arg(a.name) .. " is defined" end,
  list_symbols = function(a) return "Searching symbols for " .. quote_arg(a.query) end,
  outline_file = function(a) return "Outlining " .. tostring(a.path) end,
  find_references = function(a) return "Finding uses of " .. quote_arg(a.name) end,
  git_log = function(a)
    return "Checking git history" .. (a.path and (" for " .. tostring(a.path)) or "")
  end,
  git_blame = function(a) return "Blaming " .. tostring(a.path) end,
  git_diff = function(a) return "Diffing " .. (a.path and tostring(a.path) or "the project") end,
  git_show = function(a) return "Showing commit " .. tostring(a.commit or a.hash) end,
  working_tree_diff = function() return "Checking uncommitted changes" end,
  grep_history = function(a) return "Searching history for " .. quote_arg(a.term or a.pattern) end,
  code_owners = function(a) return "Checking who owns " .. tostring(a.path) end,
  commit_stats = function() return "Finding the most-changed files" end,
  run_tests = function() return "Running tests" end,
  build_check = function() return "Building and vetting the project" end,
  lint_check = function() return "Running the linter" end,
  list_dependencies = function() return "Listing dependencies" end,
  module_graph = function() return "Mapping the package graph" end,
  read_doc_comment = function(a) return "Reading the doc comment for " .. quote_arg(a.name) end,
  find_callers = function(a) return "Finding callers of " .. quote_arg(a.name) end,
  find_dead_code = function() return "Scanning for unused code" end,
  impact_analysis = function(a)
    return "Tracing the impact of changing " .. quote_arg(a.name)
  end,
  semantic_search = function(a) return "Searching for " .. quote_arg(a.query) end,
}

local function tool_verb(name, args)
  args = args or {}
  local fn = tool_verbs[name]
  if fn then
    local ok, label = pcall(fn, args)
    if ok and label and label ~= "" then
      return label
    end
  end
  return "Running " .. tostring(name)
end

-- chat_flush_answer re-wraps and re-writes the answer accumulated so
-- far. It's called on a debounce timer (see M._chat_on_token), never
-- once per token: a streamed answer can run into the hundreds of
-- tokens (more for a reasoning model's thinking trace), and
-- re-wrapping + re-rendering the whole growing block on every single
-- delta is O(tokens²) work for no visible benefit at typical
-- streaming rates — batching to ~25 redraws/sec is imperceptible and
-- keeps this O(tokens) instead.
local function chat_flush_answer()
  local chat = M._chat
  if not chat or not chat_valid() then
    return
  end
  chat.render_pending = false

  local wrapped = wrap_text(chat.answer_text, responseWidth - 2)
  local last = vim.api.nvim_buf_line_count(chat.buf) - 1
  vim.api.nvim_buf_set_lines(chat.buf, chat.answer_start, last, false, wrapped)

  vim.api.nvim_buf_clear_namespace(
    chat.buf, ref_ns, chat.answer_start, chat.answer_start + #wrapped + 1
  )
  chat.refs = scan_refs(chat.buf, wrapped, chat.answer_start)

  vim.bo[chat.buf].modified = false
  chat_scroll()
end

-- M._chat_on_token and M._chat_on_tool_call are armin/askEvent's two
-- event kinds, called from the handler registered in M.start().
function M._chat_on_token(delta)
  local chat = M._chat
  if not chat or not delta or delta == "" then
    return
  end

  if not chat.answer_start then
    -- The model started answering — whatever was showing as "in
    -- progress" (Thinking…, or the last tool call) is done now.
    chat_finish_active()
    chat_insert({ "" })
    chat.answer_start = vim.api.nvim_buf_line_count(chat.buf) - 1
    chat.answer_text = ""
  end

  chat.answer_text = chat.answer_text .. delta
  if not chat.render_pending then
    chat.render_pending = true
    vim.defer_fn(chat_flush_answer, 40)
  end
end

function M._chat_on_tool_call(name, args)
  if not M._chat then
    return
  end
  chat_finish_active() -- whatever was pending before this is done
  chat_set_active("  ◐ " .. tool_verb(name, args) .. "…")
  -- The next token (if any) starts a fresh answer block below this
  -- tool call, not a continuation of whatever came before it.
  M._chat.answer_start = nil
end

-- M._chat_open_ref opens/reuses a vertical split on the right showing
-- ref.file at ref.start_line, briefly flashing the cited range —
-- the "manual select" citation-preview behavior: nothing opens until
-- you put the cursor on a reference and act.
function M._chat_open_ref(ref)
  local path = ref.file
  if not path:match("^/") then
    path = (M._chat.root or vim.fn.getcwd()) .. "/" .. path
  end
  if vim.fn.filereadable(path) ~= 1 then
    vim.notify("armin: can't find " .. ref.file, vim.log.levels.WARN)
    return
  end

  local preview_win = M._chat.preview_win
  if not (preview_win and vim.api.nvim_win_is_valid(preview_win)) then
    vim.cmd("botright vsplit")
    preview_win = vim.api.nvim_get_current_win()
    M._chat.preview_win = preview_win
  else
    vim.api.nvim_set_current_win(preview_win)
  end

  vim.cmd("edit " .. vim.fn.fnameescape(path))
  vim.api.nvim_win_set_cursor(preview_win, { ref.start_line, 0 })
  vim.cmd("normal! zz")

  local flash_ns = vim.api.nvim_create_namespace("armin_chat_flash")
  local buf = vim.api.nvim_win_get_buf(preview_win)
  for line = ref.start_line, ref.end_line do
    vim.api.nvim_buf_add_highlight(buf, flash_ns, "IncSearch", line - 1, 0, -1)
  end
  vim.defer_fn(function()
    if vim.api.nvim_buf_is_valid(buf) then
      vim.api.nvim_buf_clear_namespace(buf, flash_ns, 0, -1)
    end
  end, 600)

  vim.api.nvim_set_current_win(M._chat.win)
end

-- M._chat_enter is <CR>'s handler across the whole chat buffer: on a
-- highlighted reference, opens its preview; everywhere else (the
-- prompt buffer's own last line included), falls back to Neovim's
-- normal <CR>.
function M._chat_enter()
  local chat = M._chat
  if not chat then
    return "<CR>"
  end
  local row, col = unpack(vim.api.nvim_win_get_cursor(chat.win))
  row = row - 1
  for _, ref in ipairs(chat.refs or {}) do
    if ref.row == row and col >= ref.col_start and col < ref.col_end then
      M._chat_open_ref(ref)
      return ""
    end
  end
  return "<CR>"
end

function M._chat_submit(text)
  local chat = M._chat
  if not chat or text == "" then
    return
  end
  if chat.busy then
    vim.notify("armin: still answering the previous question", vim.log.levels.WARN)
    return
  end
  local client = get_client()
  if not client then
    vim.notify("armin: not attached — run :ArminLsp first", vim.log.levels.WARN)
    return
  end

  local question = text
  local mentions = extract_mentions(text)
  if #mentions > 0 then
    question = "Referenced files: " .. table.concat(mentions, ", ") .. "\n\n" .. text
  end

  chat.busy = true
  chat.answer_start = nil
  chat_set_active("  ◐ Thinking…")

  client.request("workspace/executeCommand", {
    command = "armin.ask",
    arguments = { { question = question, uri = chat.src_uri or "" } },
  }, function(err, result)
    chat.busy = false
    if not chat_valid() then
      return
    end
    -- Whatever was still marked "in progress" (a tool call with no
    -- follow-up event, or the Thinking… placeholder if nothing ever
    -- streamed at all) is definitely over now.
    chat_finish_active()
    if chat.render_pending then
      chat_flush_answer() -- flush whatever the debounce timer hadn't yet
    end
    if err then
      chat_insert({ "  ✗ " .. (err.message or tostring(err)) })
    elseif not chat.answer_start then
      -- No armin/askEvent tokens arrived at all (e.g. a cached or
      -- instant answer) — fall back to rendering the final result
      -- directly so the question never comes back empty-handed.
      chat_insert(wrap_text(result or "(no answer)", responseWidth - 2))
    end
    chat_insert({ "" })
  end, 0)
end

-- M._open_chat opens the persistent chat window, or focuses it (and
-- retargets it at the given buffer, for a follow-up :ArminAsk from a
-- different file) if it's already open — one chat session per Neovim
-- instance, same as get_client() assumes one armin client.
function M._open_chat(root, src_uri)
  if chat_valid() then
    M._chat.src_uri = src_uri
    M._chat.root = root
    vim.api.nvim_set_current_win(M._chat.win)
    vim.cmd("startinsert!")
    return
  end

  local buf = vim.api.nvim_create_buf(false, true)
  vim.bo[buf].buftype = "prompt"
  vim.bo[buf].filetype = "markdown"
  vim.fn.prompt_setprompt(buf, "❯ ")
  -- Every edit — armin's own streamed updates, or you just typing a
  -- question and not submitting it yet — marks a normal editable
  -- buffer "modified", and Neovim refuses to :q/:qa over that. This
  -- buffer is scratch UI, never meant to be written, so nothing
  -- should ever block quitting over it. BufModifiedSet turned out
  -- not to fire reliably for programmatic edits (nvim_buf_set_lines
  -- et al, not just real keystrokes) — verified by test, not assumed
  -- — so this instead clears the flag on QuitPre, which fires before
  -- :q/:qa's own unsaved-changes check runs, catching it right at
  -- the one moment that actually matters regardless of what set it
  -- or when. The explicit clears in chat_insert/chat_set_active/
  -- chat_finish_active/chat_flush_answer stay too, so the buffer
  -- reads as unmodified the whole time, not just at quit.
  vim.bo[buf].modified = false
  -- Not buffer-scoped on purpose: QuitPre fires against whichever
  -- buffer/window is current when :q/:qa runs, which usually isn't
  -- this one — a `buffer = buf` filter would silently never fire for
  -- the common case of quitting from your actual code window with
  -- the chat window merely open in the background.
  vim.api.nvim_create_autocmd("QuitPre", {
    callback = function()
      if M._chat and vim.api.nvim_buf_is_valid(M._chat.buf) then
        vim.bo[M._chat.buf].modified = false
      end
    end,
  })

  local width = math.min(responseWidth + 4, vim.o.columns - 4)
  local height = math.min(math.floor(vim.o.lines * 0.7), vim.o.lines - 4)

  local win = vim.api.nvim_open_win(buf, true, {
    relative = "editor",
    width = width,
    height = height,
    row = math.floor((vim.o.lines - height) / 2),
    col = math.floor((vim.o.columns - width) / 2),
    style = "minimal",
    border = "rounded",
    title = " armin chat — <C-f> mention file, <Esc> close ",
    title_pos = "center",
  })
  vim.wo[win].wrap = true
  vim.wo[win].linebreak = true

  M._chat = {
    buf = buf, win = win, root = root, src_uri = src_uri,
    busy = false, answer_start = nil, answer_text = "",
    render_pending = false, refs = {}, preview_win = nil,
    active_row = nil,
  }

  vim.fn.prompt_setcallback(buf, M._chat_submit)

  -- Paste (registers, bracketed paste, <C-r> in insert mode) needs
  -- nothing special here — it's a plain prompt-buffer, so every
  -- normal insert-mode editing path Neovim supports already works on
  -- its one editable (prompt) line, same as _open_prompt before it.
  vim.keymap.set({ "i", "n" }, "<Esc>", function()
    if vim.api.nvim_win_is_valid(win) then
      vim.api.nvim_win_close(win, true)
    end
  end, { buffer = buf, nowait = true })
  vim.keymap.set("i", "<C-f>", function()
    M._insert_file_mention(buf, root)
  end, { buffer = buf })
  vim.keymap.set("n", "<CR>", M._chat_enter, { buffer = buf, expr = true, nowait = true })

  vim.cmd("startinsert!")
end

-- Ask a free-form question about the project. Opens (or focuses) the
-- persistent chat window; pass a string (as :ArminAsk itself does
-- when given args) to send it immediately instead of leaving the
-- prompt line for interactive typing.
function M.ask(question)
  local client = get_client()
  if not client then
    vim.notify("armin: not attached — run :ArminLsp first", vim.log.levels.WARN)
    return
  end

  local src_uri = vim.uri_from_bufnr(0)
  local root = find_root(vim.api.nvim_buf_get_name(0))
  M._open_chat(root, src_uri)

  if question then
    M._chat_submit(question)
  end
end

-- _select_float is a minimal floating-window picker: j/k or the
-- arrow keys move, <CR> chooses, <Esc>/q cancels. Used instead of
-- vim.ui.select, whose default (no picker plugin installed)
-- fallback renders through the command-line/message area — which
-- can be invisible with a shrunk 'cmdheight' or otherwise easy to
-- miss, exactly the kind of thing a minimal config tends to have.
-- This is a real nvim_open_win floating window, so it's always
-- visible the same way the ask/response windows already are.
function M._select_float(items, opts, on_choice)
  opts = opts or {}
  local format_item = opts.format_item or tostring

  if #items == 0 then
    on_choice(nil)
    return
  end

  local lines, width = {}, 20
  for i, item in ipairs(items) do
    lines[i] = format_item(item)
    width = math.max(width, #lines[i])
  end
  width = math.min(width + 2, vim.o.columns - 4)
  local height = math.min(#lines, vim.o.lines - 6)

  local buf = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false

  -- title_pos is only valid alongside a title — nvim_open_win
  -- errors if it's set with no title, so opts.prompt being absent
  -- must drop both, not just leave title as nil.
  local win_opts = {
    relative = "editor",
    width = width,
    height = height,
    row = math.floor((vim.o.lines - height) / 2),
    col = math.floor((vim.o.columns - width) / 2),
    style = "minimal",
    border = "rounded",
  }
  if opts.prompt then
    win_opts.title = " " .. opts.prompt .. " "
    win_opts.title_pos = "center"
  end

  local win = vim.api.nvim_open_win(buf, true, win_opts)
  vim.wo[win].cursorline = true

  local done = false
  local function finish(choice)
    if done then
      return
    end
    done = true
    if vim.api.nvim_win_is_valid(win) then
      vim.api.nvim_win_close(win, true)
    end
    on_choice(choice)
  end

  local map_opts = { buffer = buf, nowait = true, silent = true }
  vim.keymap.set("n", "<CR>", function()
    finish(items[vim.api.nvim_win_get_cursor(win)[1]])
  end, map_opts)
  vim.keymap.set("n", "q", function() finish(nil) end, map_opts)
  vim.keymap.set("n", "<Esc>", function() finish(nil) end, map_opts)
end

-- _insert_file_mention opens a file picker (_select_float) and
-- inserts an "@relative/path" mention at the cursor — used by the
-- chat window's own <C-f> keymap (see M._open_chat).
function M._insert_file_mention(buf, root)
  local files = list_project_files(root)
  if #files == 0 then
    vim.notify("armin: no files found (is ripgrep installed?)", vim.log.levels.WARN)
    return
  end

  M._select_float(files, { prompt = "Mention file" }, function(choice)
    if not choice or not vim.api.nvim_buf_is_valid(buf) then
      return
    end
    local row, col = unpack(vim.api.nvim_win_get_cursor(0))
    local mention = "@" .. choice .. " "
    vim.api.nvim_buf_set_text(buf, row - 1, col, row - 1, col, { mention })
    vim.api.nvim_win_set_cursor(0, { row, col + #mention })
    vim.cmd("startinsert!")
  end)
end

-- Show or switch armin's chat model, mirroring the REPL's /model.
-- With no argument, lists installed models — size-annotated, with
-- the smallest/largest coding-focused model tagged "fastest"/"best"
-- (see tag_models) — in a floating picker (_select_float), and
-- switches to whichever you pick. Note this switches the *chat*
-- model, used for :ArminAsk and hover; that requires Ollama
-- tool-calling support, which plain completion/FIM-only models
-- (like -completion-model's default) do not have — Ollama rejects
-- those outright here.
function M.model(name)
  local client = get_client()
  if not client then
    vim.notify("armin: not attached — run :ArminLsp first", vim.log.levels.WARN)
    return
  end

  if name then
    client.request("workspace/executeCommand", {
      command = "armin.model",
      arguments = { { name = name } },
    }, function(err, result)
      if err then
        vim.notify("armin: " .. (err.message or tostring(err)), vim.log.levels.ERROR)
        return
      end
      local current = result and result.current or name
      vim.notify("armin: switched to " .. current, vim.log.levels.INFO)
    end, 0)
    return
  end

  -- No "arguments" field at all here, deliberately: an empty Lua
  -- table would encode as JSON "[]", not "{}", and break decoding
  -- server-side — omitting the field entirely is valid per the LSP
  -- spec (ExecuteCommandParams.arguments is optional).
  client.request("workspace/executeCommand", {
    command = "armin.model",
  }, function(err, result)
    if err then
      vim.notify("armin: " .. (err.message or tostring(err)), vim.log.levels.ERROR)
      return
    end
    local installed = (result and result.installed) or {}
    if #installed == 0 then
      local current = result and result.current or "?"
      vim.notify("armin: current model is " .. current, vim.log.levels.INFO)
      return
    end

    local tags = tag_models(installed)
    M._select_float(installed, {
      prompt = "Switch armin model (current: " .. result.current .. ")",
      format_item = function(m)
        local label = m.name
        local size = human_size(m.size)
        if size ~= "" then
          label = label .. "  " .. size
        end
        if tags[m.name] then
          label = label .. "  [" .. tags[m.name] .. "]"
        end
        if m.name == result.current then
          label = label .. "  (current)"
        end
        return label
      end,
    }, function(choice)
      if choice and choice.name ~= result.current then
        M.model(choice.name)
      end
    end)
  end, 0)
end

--
-- Inline completion (ghost text)
--

-- has_suggestion reports whether the current buffer has a ghost-text
-- suggestion showing right now — the thing a Tab-accept keymap should
-- check before calling accept().
function M.has_suggestion(bufnr)
  bufnr = bufnr or vim.api.nvim_get_current_buf()
  return suggestion[bufnr] ~= nil
end

function M._clear_suggestion(bufnr)
  local s = suggestion[bufnr]
  if not s then
    return
  end
  vim.api.nvim_buf_del_extmark(bufnr, ns, s.id)
  suggestion[bufnr] = nil
end

function M._show_suggestion(bufnr, row, col, text)
  M._clear_suggestion(bufnr)

  local lines = vim.split(text, "\n", { plain = true })
  local first, rest = lines[1] or "", {}
  for i = 2, #lines do
    rest[#rest + 1] = { { lines[i], "Comment" } }
  end

  local id = vim.api.nvim_buf_set_extmark(bufnr, ns, row, col, {
    virt_text = { { first, "Comment" } },
    virt_text_pos = "inline",
    virt_lines = rest,
  })

  suggestion[bufnr] = { id = id, text = text, row = row, col = col }
end

-- accept inserts the current suggestion at the point it was generated
-- for and clears it. Does nothing if there's no suggestion showing.
-- Edits the buffer directly (nvim_buf_set_text) — must only ever be
-- called from a plain keymap/command callback, never from inside an
-- expr-mapping's evaluation, which textlocks against exactly this
-- (E565).
function M.accept()
  local bufnr = vim.api.nvim_get_current_buf()
  local s = suggestion[bufnr]
  if not s then
    return
  end
  M._clear_suggestion(bufnr)

  local lines = vim.split(s.text, "\n", { plain = true })
  vim.api.nvim_buf_set_text(bufnr, s.row, s.col, s.row, s.col, lines)

  local end_row = s.row + #lines - 1
  local end_col = #lines == 1 and (s.col + #lines[1]) or #lines[#lines]
  vim.api.nvim_win_set_cursor(0, { end_row + 1, end_col })
end

-- _request fetches one completion at the cursor and shows it as ghost
-- text if the cursor hasn't moved by the time the (slow, local-model)
-- answer comes back.
function M._request(bufnr)
  local client = get_client()
  if not client then
    return
  end

  local win = vim.fn.bufwinid(bufnr)
  if win == -1 then
    return
  end
  local cursor = vim.api.nvim_win_get_cursor(win)
  local row, col = cursor[1] - 1, cursor[2]

  client.request("textDocument/inlineCompletion", {
    textDocument = { uri = vim.uri_from_bufnr(bufnr) },
    position = { line = row, character = col },
  }, function(err, result)
    if err or not result then
      return
    end
    local items = result.items or result
    local item = items[1]
    if not item or not item.insertText or item.insertText == "" then
      return
    end

    -- Bail if the cursor moved since the request was sent — the
    -- suggestion no longer applies to where the user is now.
    local cur_win = vim.fn.bufwinid(bufnr)
    if cur_win == -1 then
      return
    end
    local now = vim.api.nvim_win_get_cursor(cur_win)
    if now[1] - 1 ~= row or now[2] ~= col then
      return
    end

    M._show_suggestion(bufnr, row, col, item.insertText)
  end, bufnr)
end

-- suggest is the fully manual path: fetch exactly one suggestion right
-- now, regardless of whether auto mode (:ArminComplete) is on.
function M.suggest()
  local client = get_client()
  if not client then
    vim.notify("armin: not attached — run :ArminLsp first", vim.log.levels.WARN)
    return
  end
  M._request(vim.api.nvim_get_current_buf())
end

-- _set_auto turns the debounced, fires-while-you-type ghost text on
-- or off for one buffer.
function M._set_auto(bufnr, on)
  if on == (auto_enabled[bufnr] == true) then
    return
  end

  if not on then
    auto_enabled[bufnr] = nil
    if augroups[bufnr] then
      vim.api.nvim_del_augroup_by_id(augroups[bufnr])
      augroups[bufnr] = nil
    end
    M._clear_suggestion(bufnr)
    return
  end

  auto_enabled[bufnr] = true
  local grp = vim.api.nvim_create_augroup(
    "armin_completion_" .. bufnr, { clear = true }
  )
  augroups[bufnr] = grp

  vim.api.nvim_create_autocmd({ "TextChangedI", "CursorMovedI" }, {
    group = grp,
    buffer = bufnr,
    callback = function()
      M._clear_suggestion(bufnr)
      gens[bufnr] = (gens[bufnr] or 0) + 1
      local my_gen = gens[bufnr]
      vim.defer_fn(function()
        if gens[bufnr] == my_gen then
          M._request(bufnr)
        end
      end, 300)
    end,
  })

  vim.api.nvim_create_autocmd("InsertLeave", {
    group = grp,
    buffer = bufnr,
    callback = function()
      M._clear_suggestion(bufnr)
    end,
  })
end

-- toggle_complete is :ArminComplete — the one thing in this file that,
-- once turned on, keeps doing something without being asked again
-- each time. It still only starts because you explicitly ran this.
function M.toggle_complete()
  local client = get_client()
  if not client then
    vim.notify("armin: not attached — run :ArminLsp first", vim.log.levels.WARN)
    return
  end

  local bufnr = vim.api.nvim_get_current_buf()
  local turning_on = not auto_enabled[bufnr]
  M._set_auto(bufnr, turning_on)
  vim.notify("armin: completion " .. (turning_on and "on" or "off"), vim.log.levels.INFO)
end

-- Registers :ArminLsp, :ArminStop, :ArminAsk, :ArminModel,
-- :ArminComplete, :ArminSuggest. Does not start or turn on anything
-- by itself.
--
-- opts.tab_accept (default false): if true, binds insert-mode <S-Tab>
-- to accept a showing suggestion, falling back to a literal <S-Tab>
-- when there isn't one — see docs/nvim.md for the equivalent one-line
-- keymap to add yourself instead, if you'd rather not have setup()
-- touch a keybinding at all.
function M.setup(opts)
  opts = opts or {}

  vim.api.nvim_create_user_command("ArminLsp", M.start, {
    desc = "Attach the armin LSP client to the current buffer",
  })
  vim.api.nvim_create_user_command("ArminStop", M.stop, {
    desc = "Stop the armin LSP client",
  })
  vim.api.nvim_create_user_command("ArminAsk", function(cmd_opts)
    M.ask(cmd_opts.args ~= "" and cmd_opts.args or nil)
  end, {
    nargs = "*",
    desc = "Ask armin a question about the current project",
  })
  vim.api.nvim_create_user_command("ArminModel", function(cmd_opts)
    M.model(cmd_opts.args ~= "" and cmd_opts.args or nil)
  end, {
    nargs = "*",
    desc = "Show or switch armin's chat model",
  })
  vim.api.nvim_create_user_command("ArminComplete", M.toggle_complete, {
    desc = "Toggle armin's automatic ghost-text completion for this buffer",
  })
  vim.api.nvim_create_user_command("ArminSuggest", M.suggest, {
    desc = "Request one armin completion at the cursor, right now",
  })

  if opts.tab_accept then
    -- Deliberately not an expr-mapping: accept() edits the buffer
    -- directly (nvim_buf_set_text), and Neovim's textlock forbids
    -- that from inside expr evaluation (E565). Shift-Tab has no
    -- default insert-mode behavior, so there's nothing to fall back
    -- to when there's no suggestion showing — doing nothing is
    -- correct.
    vim.keymap.set("i", "<S-Tab>", function()
      if M.has_suggestion() then
        M.accept()
      end
    end, { silent = true, desc = "Accept armin ghost-text suggestion" })
  end
end

return M
