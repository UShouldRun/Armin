-- Minimal Neovim integration for armin's LSP mode.
--
-- Nothing in this file runs on its own: loading it (require("armin"))
-- does nothing, and setup() only registers user commands — it does
-- not start armin, attach anything, or turn completion on. The armin
-- process only starts when you run :ArminLsp yourself, and stops when
-- you run :ArminStop. Ghost-text completion is the one thing here
-- that runs on a timer once turned on — and it only turns on when you
-- run :ArminComplete; nothing fires before that.
--
-- The chat (:ArminAsk) is one conversation per armin process, shared
-- with any `armin` started in a terminal in the same project: both
-- see every question and answer as it streams.
--
-- Requires Neovim >= 0.10 (vim.lsp.get_clients, client.request).

local M = {}

local client_name = "armin"
local ns          = vim.api.nvim_create_namespace("armin_completion")
local tip_ns      = vim.api.nvim_create_namespace("armin_chat_tip")

-- Options from setup(); see M.setup.
local config = {
  debounce = 120,     -- ms of typing pause before a completion request
  layout   = "float", -- chat window: "float" or "right" (a split)
}

-- bufnr -> { id, text, row, col }: the ghost-text suggestion currently
-- shown in that buffer, if any.
local suggestion = {}
-- bufnr -> true while auto ghost-text (:ArminComplete) is on for it.
local auto_enabled = {}
-- bufnr -> augroup id backing that buffer's auto mode.
local augroups = {}
-- bufnr -> generation counter: a scheduled request only fires if
-- it's still the latest one scheduled for that buffer.
local gens = {}
-- Completion errors already shown, so one failing model doesn't
-- notify on every keystroke.
local warned = {}

local function find_root(start)
  local markers = { ".git", "go.mod", "package.json", "pyproject.toml", "Cargo.toml" }
  local dir     = vim.fs.dirname(start)
  local found   = vim.fs.find(markers, { path = dir, upward = true })[1]
  if found then
    return vim.fs.dirname(found)
  end

  return vim.fn.getcwd()
end

local function get_client()
  return vim.lsp.get_clients({ name = client_name })[1]
end

local function not_attached()
  vim.notify("armin: not attached — run :ArminLsp first", vim.log.levels.WARN)
end

-- wrap_text hard-wraps each paragraph of text to width columns,
-- preserving existing blank-line paragraph breaks.
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

-- list_project_files shells out to rg rather than a slower Lua
-- directory walk.
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

-- cost_label is a hosted model's price: per 1M tokens in / out and
-- a deep session (~1.2M in, 30k out, as armin runs one).
local function cost_label(c)
  if not c or ((c["in"] or 0) == 0 and (c.out or 0) == 0) then
    return "free"
  end

  return string.format("$%.2f / $%.2f per 1M · ~$%.2f a session", c["in"] or 0, c.out or 0, c.session or 0)
end

-- model_state is a models.<name> entry's tag in the picker: where it
-- runs and what a use costs.
local function model_state(m)
  if m.suggested then
    return cost_label(m.cost)
  end

  if not m.managed then
    return m.cost and ("hosted · " .. cost_label(m.cost)) or "hosted"
  end

  if m.running then
    return "local, free · running"
  end

  return "local, free · spawns on use"
end

-- balance_label is what's left on the OpenRouter account.
local function balance_label(b)
  if not b then
    return nil
  end

  local text = string.format(
    "OpenRouter: $%.2f left of $%.2f · today $%.2f",
    (b.credits or 0) - (b.used or 0), b.credits or 0, b.today or 0
  )
  if (b.key_limit or 0) > 0 then
    text = text .. string.format(" · key cap $%.2f left", b.key_left or 0)
  end

  return text
end

-- extract_mentions finds "@relative/path" references in a question,
-- to nudge the model to look at them.
local function extract_mentions(text)
  local mentions = {}
  for path in text:gmatch("@([%w%._%-/]+)") do
    mentions[#mentions + 1] = path
  end

  return mentions
end

-- execute runs one armin.* workspace command. args, when given, is
-- the single argument object; omitted entirely otherwise, since an
-- empty Lua table encodes as "[]" and breaks decoding server-side.
local function execute(command, args, on_result)
  local client = get_client()
  if not client then
    not_attached()
    return
  end

  local params = { command = command }
  if args then
    params.arguments = { args }
  end

  return client.request("workspace/executeCommand", params, function(err, result)
    if on_result then
      on_result(err, result)
    elseif err then
      vim.notify("armin: " .. (err.message or tostring(err)), vim.log.levels.ERROR)
    end
  end, 0)
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
    name     = client_name,
    cmd      = { "armin", "-lsp", "-root", root },
    root_dir = root,
    -- Full-text sync on every change would encode the whole buffer
    -- per keystroke; requests flush pending changes anyway.
    flags    = { debounce_text_changes = 80 },
    handlers = {
      ["armin/askEvent"] = function(_, result)
        if result then
          M._on_event(result)
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
  if M._chat then
    M._chat_reset_turn()
    M._chat_note("armin stopped — the next question starts a new conversation")
  end

  vim.notify("armin: stopped", vim.log.levels.INFO)
end

-- responseWidth is the hard-wrap column for chat answers.
local responseWidth = 78

-- ref_ns highlights [[path:line]] / [[path:start-end]] citations
-- armin's answer includes when flagging something worth attention.
local ref_ns = vim.api.nvim_create_namespace("armin_chat_refs")

vim.api.nvim_set_hl(0, "ArminRef", { link = "Underlined", default = true })
vim.api.nvim_set_hl(0, "ArminTip", { link = "Comment", default = true })

-- ref_pattern matches the raw citation markup wherever it lands in
-- wrapped output; the markup has no spaces, so wrapping never splits
-- one.
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

-- scan_refs finds every citation in lines (already written to buf
-- from line_offset), highlights each and returns them with their
-- buffer positions.
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


--
-- Chat window
--

-- tips rotate on the "working" line while armin thinks or runs
-- tools; /tips lists them all.
local tips = {
  "<C-c> stops the answer, whoever asked",
  "/clear or :ArminClear starts the conversation over",
  "/model lists models; /model <name> switches",
  "<C-f> inserts an @file mention",
  "<CR> on a [[file:line]] reference opens it on the right",
  "<Esc> hides this window; :ArminAsk brings it back as you left it",
  "armin in a terminal in this project joins this same chat",
  ":ArminComplete toggles ghost-text completion in a buffer",
  ":ArminSuggest asks for one completion right now",
  "K explains the code under the cursor",
  "/tips lists every tip",
}

-- tip_every rotates the tip shown while armin works.
local tip_every = 12000

-- chat_valid: the chat buffer exists. Its window may be closed —
-- the conversation keeps rendering into the hidden buffer.
local function chat_valid()
  return M._chat ~= nil and vim.api.nvim_buf_is_valid(M._chat.buf)
end

local function chat_win_valid()
  return chat_valid() and M._chat.win ~= nil and vim.api.nvim_win_is_valid(M._chat.win)
end

local function chat_scroll()
  if not chat_win_valid() then
    return
  end

  local n = vim.api.nvim_buf_line_count(M._chat.buf)
  pcall(vim.api.nvim_win_set_cursor, M._chat.win, { n, 0 })
end

-- chat_insert appends lines just above the prompt buffer's trailing
-- prompt line.
local function chat_insert(lines)
  if not chat_valid() then
    return
  end

  local last = vim.api.nvim_buf_line_count(M._chat.buf) - 1
  vim.api.nvim_buf_set_lines(M._chat.buf, last, last, false, lines)
  vim.bo[M._chat.buf].modified = false
  chat_scroll()
end

-- show_tip puts the current tip after the working line, as virtual
-- text: never part of the conversation's text.
local function show_tip()
  local chat = M._chat
  if not chat_valid() then
    return
  end

  vim.api.nvim_buf_clear_namespace(chat.buf, tip_ns, 0, -1)
  if not chat.active_row or not chat.busy then
    return
  end

  local tip = tips[(chat.tip % #tips) + 1]
  pcall(vim.api.nvim_buf_set_extmark, chat.buf, tip_ns, chat.active_row, 0, {
    virt_text     = { { "  · tip: " .. tip, "ArminTip" } },
    virt_text_pos = "eol",
  })
end

local function start_tips()
  local chat = M._chat
  if chat.timer then
    return
  end

  chat.tip   = (chat.tip or 0) + 1
  chat.timer = (vim.uv or vim.loop).new_timer()
  chat.timer:start(tip_every, tip_every, vim.schedule_wrap(function()
    if M._chat and M._chat.busy then
      M._chat.tip = M._chat.tip + 1
      show_tip()
    end
  end))
end

local function stop_tips()
  local chat = M._chat
  if chat and chat.timer then
    chat.timer:stop()
    chat.timer:close()
    chat.timer = nil
  end

  if chat_valid() then
    vim.api.nvim_buf_clear_namespace(chat.buf, tip_ns, 0, -1)
  end
end

-- chat_set_active shows (or updates in place) the one "something is
-- happening right now" line, with a tip beside it.
local function chat_set_active(text)
  local chat = M._chat
  if not chat_valid() then
    return
  end

  if chat.active_row then
    vim.api.nvim_buf_set_lines(chat.buf, chat.active_row, chat.active_row + 1, false, { text })
    vim.bo[chat.buf].modified = false
  else
    chat_insert({ text })
    chat.active_row = vim.api.nvim_buf_line_count(chat.buf) - 2
  end

  show_tip()
  chat_scroll()
end

-- chat_finish_active flips the current active line's ◐ to ✓ and
-- stops tracking it.
local function chat_finish_active()
  local chat = M._chat
  if not chat_valid() or not chat.active_row then
    return
  end

  local line = (vim.api.nvim_buf_get_lines(chat.buf, chat.active_row, chat.active_row + 1, false))[1] or ""
  vim.api.nvim_buf_set_lines(
    chat.buf, chat.active_row, chat.active_row + 1, false, { (line:gsub("◐", "✓", 1)) }
  )
  vim.api.nvim_buf_clear_namespace(chat.buf, tip_ns, 0, -1)
  vim.bo[chat.buf].modified = false
  chat.active_row = nil
end

-- quote_arg and join_args back tool_verb's per-tool labels below.
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
  read_pdf = function(a) return "Reading PDF " .. tostring(a.path) end,
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
  build_check = function() return "Building the project" end,
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
  edit_file = function(a) return "Editing " .. tostring(a.path) end,
  write_file = function(a) return "Writing " .. tostring(a.path) end,
  memory_save = function(a) return "Remembering " .. quote_arg(a.name) end,
  memory_read = function(a) return "Recalling " .. quote_arg(a.name) end,
  memory_delete = function(a) return "Forgetting " .. quote_arg(a.name) end,
  scratchpad_write = function(a) return "Noting in scratchpad " .. quote_arg(a.name) end,
  scratchpad_read = function(a) return "Reading scratchpad " .. quote_arg(a.name) end,
  scratchpad_list = function() return "Listing the scratchpad" end,
  run_program = function(a) return "Running " .. tostring(a.binary) end,
  request_access = function(a) return "Asking to read " .. tostring(a.dir) end,
  use_skill = function(a)
    return "Loading skill " .. quote_arg(a.name) .. (a.file and (" (" .. tostring(a.file) .. ")") or "")
  end,
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
-- far — on a ~25/s debounce, never per token, which would be
-- O(tokens²) re-wrapping for no visible benefit.
local function chat_flush_answer()
  local chat = M._chat
  if not chat_valid() then
    return
  end

  chat.render_pending = false
  if not chat.answer_start then
    return
  end

  local wrapped = wrap_text(chat.answer_text, responseWidth - 2)
  local last    = vim.api.nvim_buf_line_count(chat.buf) - 1
  vim.api.nvim_buf_set_lines(chat.buf, chat.answer_start, last, false, wrapped)

  vim.api.nvim_buf_clear_namespace(chat.buf, ref_ns, chat.answer_start, chat.answer_start + #wrapped + 1)
  vim.list_extend(chat.refs, scan_refs(chat.buf, wrapped, chat.answer_start))

  vim.bo[chat.buf].modified = false
  chat_scroll()
end

-- M._chat_note adds a dim one-line note to the conversation.
function M._chat_note(text)
  chat_insert({ "  " .. text, "" })
end

-- M._chat_reset_turn forgets a turn in flight (stopped client,
-- cleared conversation).
function M._chat_reset_turn()
  local chat = M._chat
  if not chat then
    return
  end

  stop_tips()
  chat.busy, chat.origin, chat.answer_start, chat.active_row = false, nil, nil, nil
  chat.render_pending, chat.answer_text = false, ""
end

-- split_trace splits an armin error string into the message to show
-- and the "... in Func" chain kept for :ArminTrace. The message joins
-- each frame's own words, without the function names. Mirrors
-- ui.SplitTrace on the server.
local trace_marker = "\n\t... in "

local function trace_frames(s)
  local frames, from = {}, 1
  local prefix = s
  local first = s:find(trace_marker, 1, true)
  if first then prefix = s:sub(1, first - 1) end
  while true do
    local i = s:find(trace_marker, from, true)
    if not i then break end
    local j = s:find(trace_marker, i + 1, true)
    local part = s:sub(i + #trace_marker, j and j - 1 or #s)
    local fn, text = part:match("^(.-):\n\t(.*)$")
    table.insert(frames, { fn = fn or part, text = vim.trim(text or "") })
    from = i + 1
  end
  return vim.trim(prefix), frames
end

local function split_trace(s)
  if not s:find(trace_marker, 1, true) then
    return s, ""
  end

  local prefix, frames = trace_frames(s)
  local words = {}
  if prefix ~= "" then table.insert(words, prefix) end
  for _, f in ipairs(frames) do
    if f.text ~= "" then table.insert(words, f.text) end
  end
  return table.concat(words, " "), s
end

-- M.trace shows the last error's chain in a scratch split, one frame
-- per line, outermost first.
function M.trace()
  local s = M._last_trace
  if not s or s == "" then
    vim.notify("armin: no error to expand yet")
    return
  end

  local prefix, frames = trace_frames(s)
  local lines = {}
  if prefix ~= "" then table.insert(lines, prefix) end
  for _, f in ipairs(frames) do
    local line = "... in " .. f.fn
    if f.text ~= "" then line = line .. ": " .. f.text:gsub("\n\t", " ") end
    table.insert(lines, line)
  end

  vim.cmd("botright new")
  local buf = vim.api.nvim_get_current_buf()
  vim.bo[buf].buftype, vim.bo[buf].bufhidden = "nofile", "wipe"
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
end

-- finish_turn closes a turn on its "done" or "error" event.
local function finish_turn(e)
  local chat = M._chat
  chat_finish_active()
  if chat.render_pending then
    chat_flush_answer()
  end

  if e.type == "error" then
    if e.delta == "stopped" then
      chat_insert({ "  ■ stopped" })
    else
      local message, trace = split_trace(e.delta or "failed")
      M._last_trace = trace
      if trace ~= "" then message = message .. "  (:ArminTrace to expand)" end
      chat_insert({ "  ✗ " .. message })
    end
  elseif not chat.answer_start and e.delta and e.delta ~= "" then
    -- Nothing streamed: render the final answer directly.
    chat_insert(wrap_text(e.delta, responseWidth - 2))
  end

  chat_insert({ "" })
  M._chat_reset_turn()
end

-- clear_buffer empties the conversation, leaving the prompt line.
local function clear_buffer(note)
  local chat = M._chat
  if not chat_valid() then
    return
  end

  M._chat_reset_turn()
  local last = vim.api.nvim_buf_line_count(chat.buf) - 1
  vim.api.nvim_buf_set_lines(chat.buf, 0, last, false, { "  " .. note, "" })
  vim.api.nvim_buf_clear_namespace(chat.buf, ref_ns, 0, -1)
  chat.refs = {}
  vim.bo[chat.buf].modified = false
  chat_scroll()
end

-- M._on_event handles one armin/askEvent: the shared chat's stream,
-- for questions from this window and from attached terminals alike.
function M._on_event(e)
  local chat = M._chat
  if not chat_valid() then
    return
  end

  local t = e.type
  if t == "user" then
    if e.origin ~= "nvim" then
      chat_insert({ "  " .. (e.origin or "terminal") .. " asked:", "❯ " .. (e.delta or "") })
    end

    chat.busy, chat.origin, chat.answer_start = true, e.origin, nil
    chat_set_active("  ◐ Thinking…")
    start_tips()
    show_tip()
  elseif t == "model" then
    M._set_title(e.model)
    chat_insert({ "  now using " .. tostring(e.model and e.model.current), "" })
  elseif t == "cleared" then
    clear_buffer(e.origin == "nvim" and "conversation cleared"
      or ("conversation cleared from " .. tostring(e.origin)))
  elseif t == "done" or t == "error" then
    finish_turn(e)
  elseif t == "token" then
    M._chat_on_token(e.delta)
  elseif t == "tool_call" then
    M._chat_on_tool_call(e.name, e.args)
  elseif t == "reasoning" then
    chat.answer_start = nil
    local row  = chat.active_row
    local line = row and vim.api.nvim_buf_get_lines(chat.buf, row, row + 1, false)[1]
    if line ~= "  ◐ Thinking…" then
      chat_finish_active()
      chat_set_active("  ◐ Thinking…")
    end
  elseif t == "notice" then
    chat_finish_active()
    chat_insert({ "  ⚙ context: " .. (e.delta or "") })
    chat.answer_start = nil
  elseif t == "tool_result" then
    M._chat_on_tool_result(e.delta)
  elseif t == "usage" then
    M._chat_on_usage(e.tokens, e.window, e.tier)
  end
end

function M._chat_on_token(delta)
  local chat = M._chat
  if not delta or delta == "" then
    return
  end

  if not chat.answer_start then
    -- The model started answering: whatever was "in progress" is done.
    chat_finish_active()
    chat_insert({ "" })
    chat.answer_start = vim.api.nvim_buf_line_count(chat.buf) - 1
    chat.answer_text  = ""
  end

  chat.answer_text = chat.answer_text .. delta
  if not chat.render_pending then
    chat.render_pending = true
    vim.defer_fn(chat_flush_answer, 40)
  end
end

function M._chat_on_tool_call(name, args)
  chat_finish_active()
  chat_set_active("  ◐ " .. tool_verb(name, args) .. "…")
  -- The next token starts a fresh answer block below this call.
  M._chat.answer_start = nil
end

-- M._chat_on_tool_result closes the running tool's status line with
-- what it returned: "✓ Reading stats.go — 13 lines", or ✗ on error.
function M._chat_on_tool_result(summary)
  local chat = M._chat
  if not chat.active_row or not summary then
    return
  end

  local row  = chat.active_row
  local line = (vim.api.nvim_buf_get_lines(chat.buf, row, row + 1, false))[1] or ""
  local mark = summary:match("^error:") and "✗" or "✓"
  line = line:gsub("◐", mark, 1):gsub("…$", "") .. " — " .. summary

  vim.api.nvim_buf_set_lines(chat.buf, row, row + 1, false, { line })
  vim.api.nvim_buf_clear_namespace(chat.buf, tip_ns, 0, -1)
  vim.bo[chat.buf].modified = false
  chat.active_row = nil
end

-- k_tokens renders a token count compactly: 950, 8.4k, 96k.
local function k_tokens(n)
  if n < 1000 then
    return tostring(n)
  elseif n < 10000 then
    return string.format("%.1fk", n / 1000)
  end

  return string.format("%dk", math.floor(n / 1000))
end

-- M._chat_on_usage shows how full the context window is in the chat
-- window's bottom border, highlighted as compaction (75%) nears.
function M._chat_on_usage(tokens, window, tier)
  local chat = M._chat
  if not tokens then
    return
  end

  local text = " context " .. k_tokens(tokens)
  local hl   = "Comment"
  if window and window > 0 then
    local pct = math.floor(tokens * 100 / window)
    text = text .. " / " .. k_tokens(window) .. " · " .. pct .. "%"
    if pct >= 75 then
      hl = "ErrorMsg"
    elseif pct >= 50 then
      hl = "WarningMsg"
    end
  end

  if tier and tier ~= "" then
    text = text .. " · " .. tier
  end

  chat.usage = { text .. " ", hl }
  if chat_win_valid() and vim.api.nvim_win_get_config(chat.win).relative ~= "" then
    vim.api.nvim_win_set_config(chat.win, { footer = { chat.usage }, footer_pos = "right" })
  end
end

-- M._chat_open_ref opens/reuses a vertical split showing ref.file
-- at ref.start_line, briefly flashing the cited range.
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
    vim.cmd(config.layout == "right" and "topleft vsplit" or "botright vsplit")
    preview_win = vim.api.nvim_get_current_win()
    M._chat.preview_win = preview_win
  else
    vim.api.nvim_set_current_win(preview_win)
  end

  vim.cmd("edit " .. vim.fn.fnameescape(path))
  vim.api.nvim_win_set_cursor(preview_win, { ref.start_line, 0 })
  vim.cmd("normal! zz")

  local flash_ns = vim.api.nvim_create_namespace("armin_chat_flash")
  local buf      = vim.api.nvim_win_get_buf(preview_win)
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

-- M._chat_enter is <CR>'s handler in normal mode: on a reference,
-- opens its preview; anywhere else, Neovim's normal <CR>.
function M._chat_enter()
  local chat = M._chat
  if not chat_win_valid() then
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

-- M.stop_answer stops the turn in flight, whoever asked: <C-c> in
-- the chat, :ArminCancel.
function M.stop_answer()
  if M._chat and M._chat.busy then
    execute("armin.stop")
  end
end

-- M.clear forgets the conversation, here and in attached terminals.
function M.clear()
  execute("armin.clear", nil, function(err)
    if err then
      vim.notify("armin: " .. (err.message or tostring(err)), vim.log.levels.WARN)
    end
  end)
end

-- chat_command runs a /command typed at the chat prompt.
local function chat_command(text)
  local name, arg = text:match("^/(%S+)%s*(.-)%s*$")
  if name == "clear" then
    M.clear()
  elseif name == "model" then
    M.model(arg ~= "" and arg or nil)
  elseif name == "stop" then
    M.stop_answer()
  elseif name == "tips" then
    local lines = { "  tips:" }
    for i, tip in ipairs(tips) do
      lines[#lines + 1] = string.format("  %2d. %s", i, tip)
    end

    lines[#lines + 1] = ""
    chat_insert(lines)
  elseif name == "help" then
    chat_insert({
      "  /clear          start the conversation over",
      "  /model [name]   list models, or switch to one",
      "  /stop           stop the answer (also <C-c>)",
      "  /tips           list tips",
      "",
    })
  else
    chat_insert({ "  unknown command /" .. tostring(name) .. " — /help lists them", "" })
  end
end

function M._chat_submit(text)
  local chat = M._chat
  text = vim.trim(text or "")
  if not chat or text == "" then
    return
  end

  if text:sub(1, 1) == "/" then
    chat_command(text)
    return
  end

  if chat.busy then
    chat_insert({ "  still answering — <C-c> stops it", "" })
    return
  end

  local question = text
  local mentions = extract_mentions(text)
  if #mentions > 0 then
    question = "Referenced files: " .. table.concat(mentions, ", ") .. "\n\n" .. text
  end

  -- Busy now, not on the server's "user" event: a fast second
  -- submit must not race it.
  chat.busy, chat.origin, chat.answer_start = true, "nvim", nil
  chat_set_active("  ◐ Thinking…")
  start_tips()

  local sent = execute("armin.ask", { question = question, uri = chat.src_uri or "" }, function(err)
    -- "done"/"error" events finish the turn; only an error the
    -- server sent no event for (busy, transport) is handled here.
    if err and chat.busy and chat.origin == "nvim" and err.code ~= -32800 then
      finish_turn({ type = "error", delta = err.message or tostring(err) })
    end
  end)
  if not sent then
    M._chat_reset_turn()
    chat_finish_active()
  end
end

-- load_history renders the conversation so far into a new buffer:
-- the chat lives in the armin process, not in this window.
local function load_history()
  execute("armin.history", nil, function(err, lines)
    if err or type(lines) ~= "table" or #lines == 0 or not chat_valid() then
      return
    end

    local out = {}
    for _, l in ipairs(lines) do
      if l.role == "user" then
        out[#out + 1] = "❯ " .. l.text
      elseif l.role == "tool" then
        out[#out + 1] = "  ✓ " .. l.text
      else
        out[#out + 1] = ""
        vim.list_extend(out, wrap_text(l.text, responseWidth - 2))
        out[#out + 1] = ""
      end
    end

    vim.api.nvim_buf_set_lines(M._chat.buf, 0, 0, false, out)
    M._chat.refs = scan_refs(M._chat.buf, out, 0)
    vim.bo[M._chat.buf].modified = false
    chat_scroll()
  end)
end

-- make_chat_buf creates the chat's prompt buffer and its keymaps.
local function make_chat_buf(root)
  local buf = vim.api.nvim_create_buf(false, true)
  vim.bo[buf].buftype   = "prompt"
  vim.bo[buf].bufhidden = "hide"
  vim.bo[buf].filetype  = "markdown"
  pcall(vim.api.nvim_buf_set_name, buf, "armin://chat")
  vim.fn.prompt_setprompt(buf, "❯ ")
  vim.fn.prompt_setcallback(buf, M._chat_submit)

  -- Scratch UI, never written: nothing should block :q over it.
  -- QuitPre fires before :q's unsaved-changes check, from whichever
  -- window quits, so it isn't buffer-scoped.
  vim.api.nvim_create_autocmd("QuitPre", {
    callback = function()
      if vim.api.nvim_buf_is_valid(buf) then
        vim.bo[buf].modified = false
      end
    end,
  })

  local function hide()
    if chat_win_valid() then
      vim.api.nvim_win_close(M._chat.win, true)
    end
  end

  local o = { buffer = buf, nowait = true, silent = true }
  vim.keymap.set({ "i", "n" }, "<Esc>", hide, o)
  vim.keymap.set("n", "q", hide, o)
  vim.keymap.set({ "i", "n" }, "<C-c>", M.stop_answer, o)
  vim.keymap.set("i", "<C-f>", function() M._insert_file_mention(buf, root) end, o)
  vim.keymap.set("n", "<CR>", M._chat_enter, { buffer = buf, expr = true, nowait = true })

  return buf
end

-- set_title names the instance in the chat window, the way the
-- terminal's banner does: "Armin local · instance 1" — the same
-- number in both means they share one conversation.
local function set_title(info)
  local chat = M._chat
  if info then
    chat.title = "Armin " .. (info.current or "?")
    if info.instance and info.instance > 0 then
      chat.title = chat.title .. " · instance " .. info.instance
    end
  end

  if not chat_win_valid() then
    return
  end

  local text = " " .. (chat.title or "Armin") .. " — <C-c> stop · /help · <Esc> hide "
  if vim.api.nvim_win_get_config(chat.win).relative ~= "" then
    vim.api.nvim_win_set_config(chat.win, { title = text, title_pos = "center" })
  else
    vim.wo[chat.win].winbar = "%#Title#" .. text:gsub("%%", "%%%%")
  end
end

M._set_title = set_title

local function refresh_title()
  execute("armin.model", nil, function(err, info)
    if not err and chat_valid() then
      set_title(info)
    end
  end)
end

-- open_chat_win shows the chat buffer: a centered float, or a split
-- on the right with layout = "right".
local function open_chat_win(buf)
  local chat  = M._chat
  local title = " " .. (chat.title or "Armin") .. " — <C-c> stop · /help · <Esc> hide "

  local win
  if config.layout == "right" then
    vim.cmd("botright vsplit")
    win = vim.api.nvim_get_current_win()
    vim.api.nvim_win_set_buf(win, buf)
    vim.api.nvim_win_set_width(win, math.min(responseWidth + 4, math.floor(vim.o.columns / 2)))
    vim.wo[win].number, vim.wo[win].relativenumber = false, false
    vim.wo[win].signcolumn, vim.wo[win].winfixwidth = "no", true
  else
    local width  = math.min(responseWidth + 4, vim.o.columns - 4)
    local height = math.min(math.floor(vim.o.lines * 0.7), vim.o.lines - 4)
    local opts   = {
      relative  = "editor",
      width     = width,
      height    = height,
      row       = math.floor((vim.o.lines - height) / 2),
      col       = math.floor((vim.o.columns - width) / 2),
      style     = "minimal",
      border    = "rounded",
      title     = title,
      title_pos = "center",
    }
    if chat.usage then
      opts.footer, opts.footer_pos = { chat.usage }, "right"
    end

    win = vim.api.nvim_open_win(buf, true, opts)
  end

  vim.wo[win].wrap      = true
  vim.wo[win].linebreak = true
  chat.win = win
  set_title()
end

-- M._open_chat shows the chat — the same buffer every time, so the
-- conversation is there exactly as you left it until /clear — and
-- retargets it at the buffer :ArminAsk was run from.
function M._open_chat(root, src_uri)
  local fresh = not chat_valid()
  if fresh then
    M._chat = {
      buf = make_chat_buf(root), root = root, busy = false,
      answer_text = "", render_pending = false, refs = {}, tip = 0,
    }
  end

  local chat = M._chat
  chat.src_uri, chat.root = src_uri, root

  if chat_win_valid() then
    vim.api.nvim_set_current_win(chat.win)
  else
    open_chat_win(chat.buf)
  end

  if fresh then
    load_history()
  end

  refresh_title()

  chat_scroll()
  vim.cmd("startinsert!")
end

-- Ask a free-form question about the project. Opens (or focuses) the
-- chat; pass a string to send it right away.
function M.ask(question)
  if not get_client() then
    not_attached()
    return
  end

  local src_uri = vim.uri_from_bufnr(0)
  local root    = find_root(vim.api.nvim_buf_get_name(0))
  if M._chat and vim.api.nvim_get_current_buf() == M._chat.buf then
    src_uri, root = M._chat.src_uri, M._chat.root
  end

  M._open_chat(root, src_uri)

  if question then
    -- Typed questions stay on the prompt line; show this one too.
    chat_insert({ "❯ " .. question })
    M._chat_submit(question)
  end
end

-- _select_float is a minimal floating-window picker: j/k or arrows
-- move, <CR> chooses, <Esc>/q cancels. Used instead of vim.ui.select,
-- whose fallback renders in the command line — easy to miss.
function M._select_float(items, opts, on_choice)
  opts = opts or {}
  local format_item = opts.format_item or tostring

  if #items == 0 then
    on_choice(nil)
    return
  end

  -- Opened from insert mode (the chat prompt), j/k would type.
  local from_win = vim.api.nvim_get_current_win()
  local was_insert = vim.fn.mode():sub(1, 1) == "i"
  vim.cmd("stopinsert")

  local lines, width = {}, 20
  for i, item in ipairs(items) do
    lines[i] = " " .. format_item(item)
    width    = math.max(width, vim.fn.strdisplaywidth(lines[i]))
  end

  width = math.min(width + 2, vim.o.columns - 4)
  local height = math.min(#lines, vim.o.lines - 6)

  local buf = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false

  -- title_pos is only valid alongside a title.
  local win_opts = {
    relative = "editor",
    width    = width,
    height   = height,
    row      = math.floor((vim.o.lines - height) / 2),
    col      = math.floor((vim.o.columns - width) / 2),
    style    = "minimal",
    border   = "rounded",
  }
  if opts.prompt then
    win_opts.title     = " " .. opts.prompt .. " "
    win_opts.title_pos = "center"
    win_opts.width     = math.min(math.max(win_opts.width, vim.fn.strdisplaywidth(win_opts.title) + 2), vim.o.columns - 4)
    win_opts.col       = math.floor((vim.o.columns - win_opts.width) / 2)
  end

  -- footer: a help bar under the list, string or {text, hl} chunks.
  if opts.footer then
    local chunks = type(opts.footer) == "table" and opts.footer or { { opts.footer, "Comment" } }
    local fw = 0
    for _, c in ipairs(chunks) do
      fw = fw + vim.fn.strdisplaywidth(c[1])
    end

    win_opts.width      = math.min(math.max(win_opts.width, fw + 2), vim.o.columns - 4)
    win_opts.col        = math.floor((vim.o.columns - win_opts.width) / 2)
    win_opts.footer     = chunks
    win_opts.footer_pos = "center"
  end

  local win = vim.api.nvim_open_win(buf, true, win_opts)
  vim.wo[win].cursorline = true
  if opts.initial then
    pcall(vim.api.nvim_win_set_cursor, win, { opts.initial, 0 })
  end

  local done = false
  local function finish(choice)
    if done then
      return
    end

    done = true
    if vim.api.nvim_win_is_valid(win) then
      vim.api.nvim_win_close(win, true)
    end

    if vim.api.nvim_win_is_valid(from_win) then
      vim.api.nvim_set_current_win(from_win)
      if was_insert then
        vim.cmd("startinsert!")
      end
    end

    on_choice(choice)
  end

  local map_opts = { buffer = buf, nowait = true, silent = true }
  vim.keymap.set("n", "<CR>", function()
    finish(items[vim.api.nvim_win_get_cursor(win)[1]])
  end, map_opts)
  vim.keymap.set("n", "q", function() finish(nil) end, map_opts)
  vim.keymap.set("n", "<Esc>", function() finish(nil) end, map_opts)
  vim.api.nvim_create_autocmd("WinLeave", {
    buffer = buf, once = true, callback = function() finish(nil) end,
  })
end

-- _insert_file_mention opens a file picker and inserts an
-- "@relative/path" mention at the cursor.
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
    local mention  = "@" .. choice .. " "
    vim.api.nvim_buf_set_text(buf, row - 1, col, row - 1, col, { mention })
    vim.api.nvim_win_set_cursor(0, { row, col + #mention })
  end)
end

-- model_names asks armin for its configured models, synchronously:
-- :ArminModel's <Tab> completion.
local function model_names(arglead)
  local client = get_client()
  if not client then
    return {}
  end

  local res  = client.request_sync("workspace/executeCommand", { command = "armin.model" }, 3000, 0)
  local info = (res and res.result) or {}
  local out, seen = {}, {}
  local function offer(name)
    if not seen[name] and name:find(arglead, 1, true) == 1 then
      seen[name] = true
      out[#out + 1] = name
    end
  end

  for _, m in ipairs(info.configured or {}) do
    offer(m.name)
  end

  -- "planner:<id>" for each OpenRouter suggestion.
  for _, sug in ipairs((info.hosted and info.suggested) or {}) do
    offer(info.hosted .. ":" .. sug.id)
  end

  return out
end

-- Show or switch armin's chat model. With no name, a picker of every
-- models.<name> entry in config.conf, the current one under the
-- cursor. Without its own completion_model, completion follows.
-- choose_hosted asks which OpenRouter model to reach through
-- hosted (like "planner"): its own id first and preselected, so <CR>
-- keeps the default, then the suggestions with their prices.
local function choose_hosted(info, hosted)
  local def
  for _, c in ipairs(info.configured or {}) do
    if c.name == hosted then
      def = c
    end
  end

  local items = { { name = hosted, id = def and def.model or hosted, cost = def and def.cost, default = true } }
  for _, sug in ipairs(info.suggested or {}) do
    if not def or sug.id ~= def.model then
      items[#items + 1] = { name = hosted .. ":" .. sug.id, id = sug.id, cost = sug }
    end
  end

  local width = 4
  for _, it in ipairs(items) do
    width = math.max(width, #it.id)
  end

  M._select_float(items, {
    prompt  = "OpenRouter model for " .. hosted,
    initial = 1,
    footer  = " <CR> on the first keeps the default · j/k move · q close · :ArminModel " .. hosted .. ":<id> any id ",
    format_item = function(it)
      return string.format(
        "%s%-" .. width .. "s  %s%s",
        it.default and "● " or "  ", it.id, cost_label(it.cost), it.default and "  (default)" or ""
      )
    end,
  }, function(choice)
    if choice then
      M._switch_model(choice.name)
    end
  end)
end

function M.model(name)
  if name then
    -- A bare OpenRouter model asks which model to reach through it.
    execute("armin.model", nil, function(err, info)
      if not err and info and info.hosted == name then
        choose_hosted(info, name)
      else
        M._switch_model(name)
      end
    end)
    return
  end

  M._list_models()
end

-- _switch_model switches the chat model to name.
function M._switch_model(name)
  vim.notify("armin: switching to " .. name .. "…", vim.log.levels.INFO)
  execute("armin.model", { name = name }, function(err, result)
    if err then
      vim.notify("armin: " .. (err.message or tostring(err)), vim.log.levels.ERROR)
      return
    end

    local msg = "armin: now using " .. (result and result.current or name)
    if result and result.completion and result.completion ~= result.current then
      msg = msg .. " (completion: " .. result.completion .. ")"
    end

    vim.notify(msg, vim.log.levels.INFO)
  end)
end

-- _list_models is the picker of every model, hosted ones priced.
function M._list_models()
  execute("armin.model", nil, function(err, result)
    if err then
      vim.notify("armin: " .. (err.message or tostring(err)), vim.log.levels.ERROR)
      return
    end

    local configured = vim.deepcopy((result and result.configured) or {})
    local current    = (result and result.current) or "?"
    local hosted     = result and result.hosted

    -- OpenRouter suggestions, switchable as "<hosted>:<id>".
    for _, sug in ipairs((hosted and result.suggested) or {}) do
      local name = hosted .. ":" .. sug.id
      local known = false
      for _, c in ipairs(configured) do
        known = known or c.name == name
      end

      if not known then
        configured[#configured + 1] = {
          name = name, model = "", suggested = true, cost = sug,
        }
      end
    end

    if #configured == 0 then
      vim.notify("armin: current model is " .. current .. " (no models.<name> in config.conf)", vim.log.levels.INFO)
      return
    end

    local initial, wname, wmodel = 1, 4, 4
    for i, m in ipairs(configured) do
      wname  = math.max(wname, #m.name)
      wmodel = math.max(wmodel, #(m.model or ""))
      if m.name == current then
        initial = i
      end
    end

    local help = " <CR> switch · j/k move · q close · :ArminModel <name> · completion stays local "
    if hosted then
      help = " <CR> switch · q close · :ArminModel " .. hosted .. ":<id> any OpenRouter model · completion stays local "
    end

    local title = "armin model"
    if result.instance and result.instance > 0 then
      title = title .. " · instance " .. result.instance
    end

    local balance = balance_label(result.balance)
    if balance then
      title = title .. " · " .. balance
    end

    M._select_float(configured, {
      prompt  = title,
      initial = initial,
      footer  = help,
      format_item = function(m)
        local mark = m.name == current and "● " or "  "
        return string.format(
          "%s%-" .. wname .. "s  %-" .. wmodel .. "s  %s",
          mark, m.name, m.model or "", model_state(m)
        )
      end,
    }, function(choice)
      if choice and choice.name ~= current then
        M.model(choice.name)
      end
    end)
  end)
end

--
-- Inline completion (ghost text)
--

-- has_suggestion reports whether the current buffer has a ghost-text
-- suggestion showing right now.
function M.has_suggestion(bufnr)
  bufnr = bufnr or vim.api.nvim_get_current_buf()
  return suggestion[bufnr] ~= nil
end

-- _clear_suggestion wipes the whole ghost-text namespace, not just
-- the mark it remembers: a mark it lost track of would otherwise
-- stay over the code for good.
function M._clear_suggestion(bufnr)
  suggestion[bufnr] = nil
  if vim.api.nvim_buf_is_valid(bufnr) then
    vim.api.nvim_buf_clear_namespace(bufnr, ns, 0, -1)
  end
end

-- in_insert: ghost text only ever shows while typing in bufnr.
local function in_insert(bufnr)
  return vim.api.nvim_get_mode().mode:sub(1, 1) == "i"
    and vim.api.nvim_get_current_buf() == bufnr
end

function M._show_suggestion(bufnr, row, col, text)
  M._clear_suggestion(bufnr)
  if text == "" or not in_insert(bufnr) then
    return
  end

  local lines = vim.split(text, "\n", { plain = true })
  local rest  = {}
  for i = 2, #lines do
    rest[#rest + 1] = { { lines[i], "Comment" } }
  end

  local ok, id = pcall(vim.api.nvim_buf_set_extmark, bufnr, ns, row, col, {
    virt_text     = { { lines[1], "Comment" } },
    virt_text_pos = "inline",
    virt_lines    = rest,
  })
  if ok then
    suggestion[bufnr] = { id = id, text = text, row = row, col = col }
  end
end

-- typed_since is the text typed on row between col and the cursor,
-- or nil when the cursor left that line or went back.
local function typed_since(bufnr, row, col)
  local win = vim.fn.bufwinid(bufnr)
  if win == -1 then
    return nil
  end

  local cur = vim.api.nvim_win_get_cursor(win)
  if cur[1] - 1 ~= row or cur[2] < col then
    return nil
  end

  local line = vim.api.nvim_buf_get_lines(bufnr, row, row + 1, false)[1] or ""

  return line:sub(col + 1, cur[2]), cur[2]
end

-- follow keeps a suggestion alive while you type what it suggests:
-- the typed part comes off its front and the rest stays shown, with
-- no new request. False when the typing diverged from it.
local function follow(bufnr, s)
  local typed, now = typed_since(bufnr, s.row, s.col)
  if not typed then
    return false
  end

  if typed == "" then
    M._show_suggestion(bufnr, s.row, s.col, s.text)
    return true
  end

  if s.text:sub(1, #typed) ~= typed or #typed >= #s.text then
    return false
  end

  M._show_suggestion(bufnr, s.row, now, s.text:sub(#typed + 1))

  return true
end

-- accept_text inserts text at the suggestion's point.
local function accept_text(bufnr, s, text)
  local lines = vim.split(text, "\n", { plain = true })
  vim.api.nvim_buf_set_text(bufnr, s.row, s.col, s.row, s.col, lines)

  local end_row = s.row + #lines - 1
  local end_col = #lines == 1 and (s.col + #lines[1]) or #lines[#lines]
  vim.api.nvim_win_set_cursor(0, { end_row + 1, end_col })
end

-- accept inserts the whole suggestion. Edits the buffer directly, so
-- never from an expr-mapping (E565).
function M.accept()
  local bufnr = vim.api.nvim_get_current_buf()
  local s = suggestion[bufnr]
  if not s then
    return
  end

  M._clear_suggestion(bufnr)
  accept_text(bufnr, s, s.text)
end

-- accept_line inserts the suggestion's first line (with its newline
-- when more follows); the rest stays shown.
function M.accept_line()
  local bufnr = vim.api.nvim_get_current_buf()
  local s = suggestion[bufnr]
  if not s then
    return
  end

  local nl = s.text:find("\n", 1, true)
  if not nl then
    return M.accept()
  end

  M._clear_suggestion(bufnr)
  accept_text(bufnr, s, s.text:sub(1, nl))

  local cur = vim.api.nvim_win_get_cursor(0)
  M._show_suggestion(bufnr, cur[1] - 1, cur[2], s.text:sub(nl + 1))
end

-- accept_word inserts up to the end of the suggestion's next word.
function M.accept_word()
  local bufnr = vim.api.nvim_get_current_buf()
  local s = suggestion[bufnr]
  if not s then
    return
  end

  local word = s.text:match("^%s*[%w_]+") or s.text:match("^%s*[^%w_%s]+") or s.text:match("^%s+")
  if not word or word == s.text then
    return M.accept()
  end

  M._clear_suggestion(bufnr)
  accept_text(bufnr, s, word)

  local cur = vim.api.nvim_win_get_cursor(0)
  M._show_suggestion(bufnr, cur[1] - 1, cur[2], s.text:sub(#word + 1))
end

-- _request fetches one completion at the cursor. An answer that
-- arrives after you typed more still shows, minus what you typed,
-- as long as your typing matches its start.
function M._request(bufnr)
  local client = get_client()
  if not client then
    return
  end

  local win = vim.fn.bufwinid(bufnr)
  if win == -1 then
    return
  end

  local cursor   = vim.api.nvim_win_get_cursor(win)
  local row, col = cursor[1] - 1, cursor[2]

  client.request("textDocument/inlineCompletion", {
    textDocument = { uri = vim.uri_from_bufnr(bufnr) },
    position     = { line = row, character = col },
  }, function(err, result)
    if err then
      -- -32800: superseded by a newer request, nothing wrong.
      if err.code ~= -32800 and not warned[err.message] then
        warned[err.message] = true
        vim.notify("armin completion: " .. tostring(err.message), vim.log.levels.WARN)
      end

      return
    end

    local items = result and (result.items or result) or {}
    local item  = items[1]
    if not item or not item.insertText or item.insertText == "" then
      return
    end

    -- Late: you left insert mode or the buffer, or are already
    -- typing through a newer one.
    if suggestion[bufnr] or not in_insert(bufnr) then
      return
    end

    follow(bufnr, { row = row, col = col, text = item.insertText })
  end, bufnr)
end

-- clear_ghosts wipes a buffer's suggestion whenever it stops
-- matching what's on screen: leaving insert mode or the window, an
-- edit outside insert mode (undo, paste), a normal-mode move. It
-- covers :ArminSuggest too, which auto mode's autocmds don't.
local ghost_group = vim.api.nvim_create_augroup("armin_ghost_text", { clear = true })
vim.api.nvim_create_autocmd(
  { "InsertLeave", "BufLeave", "WinLeave", "TextChanged", "CursorMoved" },
  {
    group    = ghost_group,
    callback = function(args)
      if suggestion[args.buf] then
        M._clear_suggestion(args.buf)
      end
    end,
  }
)

-- suggest is the fully manual path: one suggestion right now, shown
-- while you stay in insert mode.
function M.suggest()
  if not get_client() then
    not_attached()
    return
  end

  M._request(vim.api.nvim_get_current_buf())
end

-- _set_auto turns the debounced ghost text on or off for one buffer.
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
  local grp = vim.api.nvim_create_augroup("armin_completion_" .. bufnr, { clear = true })
  augroups[bufnr] = grp

  vim.api.nvim_create_autocmd({ "TextChangedI", "CursorMovedI" }, {
    group    = grp,
    buffer   = bufnr,
    callback = function()
      gens[bufnr] = (gens[bufnr] or 0) + 1
      local s = suggestion[bufnr]
      if s and follow(bufnr, s) then
        return -- typing what's suggested: no request needed
      end

      M._clear_suggestion(bufnr)
      local my_gen = gens[bufnr]
      vim.defer_fn(function()
        if gens[bufnr] == my_gen and vim.fn.mode():sub(1, 1) == "i" then
          M._request(bufnr)
        end
      end, config.debounce)
    end,
  })

  vim.api.nvim_create_autocmd("InsertLeave", {
    group    = grp,
    buffer   = bufnr,
    callback = function()
      gens[bufnr] = (gens[bufnr] or 0) + 1
      M._clear_suggestion(bufnr)
    end,
  })
end

-- toggle_complete is :ArminComplete.
function M.toggle_complete()
  if not get_client() then
    not_attached()
    return
  end

  local bufnr      = vim.api.nvim_get_current_buf()
  local turning_on = not auto_enabled[bufnr]
  M._set_auto(bufnr, turning_on)
  vim.notify("armin: completion " .. (turning_on and "on" or "off"), vim.log.levels.INFO)
end

-- Registers the :Armin* commands. Starts nothing by itself.
--
-- opts.tab_accept (default false): insert-mode <S-Tab> accepts a
--   showing suggestion, <C-Right> its next word, <C-Down> its line.
-- opts.debounce (default 120): ms of typing pause before a request.
-- opts.layout (default "float"): the chat as a float, or "right"
--   for a split on the right like a terminal pane.
function M.setup(opts)
  opts = opts or {}
  config.debounce = opts.debounce or config.debounce
  config.layout   = opts.layout or config.layout

  local function cmd(name, fn, o)
    vim.api.nvim_create_user_command(name, fn, o)
  end

  cmd("ArminLsp", M.start, { desc = "Attach the armin LSP client to the current buffer" })
  cmd("ArminStop", M.stop, { desc = "Stop the armin LSP client" })
  cmd("ArminAsk", function(o)
    M.ask(o.args ~= "" and o.args or nil)
  end, { nargs = "*", desc = "Open armin's chat, optionally asking something" })
  cmd("ArminClear", M.clear, { desc = "Start armin's chat over" })
  cmd("ArminCancel", M.stop_answer, { desc = "Stop armin's answer in flight" })
  cmd("ArminTrace", M.trace, { desc = "Show the last error's function trace" })
  cmd("ArminModel", function(o)
    M.model(o.args ~= "" and o.args or nil)
  end, { nargs = "?", complete = model_names, desc = "List or switch armin's chat model" })
  cmd("ArminComplete", M.toggle_complete, { desc = "Toggle armin's ghost-text completion here" })
  cmd("ArminSuggest", M.suggest, { desc = "Request one armin completion at the cursor" })

  if opts.tab_accept then
    -- Not expr-mappings: accepting edits the buffer, which textlock
    -- forbids inside expr evaluation (E565).
    local function when(fn, fallback)
      return function()
        if M.has_suggestion() then
          fn()
        elseif fallback then
          local keys = vim.api.nvim_replace_termcodes(fallback, true, false, true)
          vim.api.nvim_feedkeys(keys, "n", false)
        end
      end
    end

    vim.keymap.set("i", "<S-Tab>", when(M.accept), { silent = true, desc = "Accept armin suggestion" })
    vim.keymap.set("i", "<C-Right>", when(M.accept_word, "<C-Right>"), { silent = true, desc = "Accept armin suggestion's next word" })
    vim.keymap.set("i", "<C-Down>", when(M.accept_line, "<C-Down>"), { silent = true, desc = "Accept armin suggestion's line" })
  end
end

return M
