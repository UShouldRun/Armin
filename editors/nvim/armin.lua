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

-- responseWidth is the hard-wrap column for _show and the floating
-- window's own width — matches this project's own 70-80 col style
-- rather than however wide the window happens to be.
local responseWidth = 78

-- Ask a free-form question about the project. Opens a floating
-- prompt window when called with no argument — :ArminAsk alone —
-- instead of typing on the command line; pass a string (as
-- :ArminAsk itself does when given args) to skip straight to
-- sending it.
function M.ask(question)
  local client = get_client()
  if not client then
    vim.notify("armin: not attached — run :ArminLsp first", vim.log.levels.WARN)
    return
  end

  if question then
    M._send_ask(client, question)
    return
  end

  local root = find_root(vim.api.nvim_buf_get_name(0))
  M._open_prompt(root, function(text)
    if text ~= "" then
      M._send_ask(client, text)
    end
  end)
end

function M._send_ask(client, question)
  vim.notify("armin: thinking…", vim.log.levels.INFO)

  local mentions = extract_mentions(question)
  if #mentions > 0 then
    question = "Referenced files: " .. table.concat(mentions, ", ")
      .. "\n\n" .. question
  end

  -- client.request is a pre-bound closure (see :h vim.lsp.Client), not
  -- a colon-method — calling it with ":" would double-apply self and
  -- shift every argument by one, landing the handler in bufnr's slot.
  client.request("workspace/executeCommand", {
    command = "armin.ask",
    arguments = { { question = question, uri = vim.uri_from_bufnr(0) } },
  }, function(err, result)
    if err then
      vim.notify("armin: " .. (err.message or tostring(err)), vim.log.levels.ERROR)
      return
    end
    M._show(result or "(no answer)")
  end, 0)
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

-- _open_prompt is a floating input window replacing vim.fn.input's
-- command-line prompt — built on Neovim's own prompt-buffer support
-- (:h prompt-buffer), so Enter submits and normal insert-mode
-- editing (arrows, backspace, ...) works exactly as usual; no
-- hand-rolled key handling needed here, unlike the raw-mode REPL
-- ghost-text editor. <C-f> opens a file picker (_select_float) that
-- inserts an "@relative/path" mention at the cursor.
function M._open_prompt(root, on_submit)
  local buf = vim.api.nvim_create_buf(false, true)
  vim.bo[buf].buftype = "prompt"
  vim.fn.prompt_setprompt(buf, "Ask armin> ")

  local width = math.min(responseWidth - 2, vim.o.columns - 4)
  local win = vim.api.nvim_open_win(buf, true, {
    relative = "editor",
    width = width,
    height = 1,
    row = math.floor((vim.o.lines - 1) / 2),
    col = math.floor((vim.o.columns - width) / 2),
    style = "minimal",
    border = "rounded",
    title = " Ask armin — <C-f> mention file, <Esc> cancel ",
    title_pos = "center",
  })

  local function close()
    if vim.api.nvim_win_is_valid(win) then
      vim.api.nvim_win_close(win, true)
    end
  end

  vim.fn.prompt_setcallback(buf, function(text)
    close()
    on_submit(text)
  end)

  vim.keymap.set({ "i", "n" }, "<Esc>", close, { buffer = buf, nowait = true })
  vim.keymap.set({ "i", "n" }, "<C-f>", function()
    M._insert_file_mention(buf, root)
  end, { buffer = buf })

  vim.cmd("startinsert!")
end

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

-- A plain floating scratch window — no popup/UI plugin required.
-- Text is hard-wrapped to responseWidth first (see wrap_text): the
-- model's answers come back as one unbroken paragraph, and relying
-- on soft-wrap alone reads as a wall of text at typical window
-- widths.
function M._show(text)
  local lines = wrap_text(text, responseWidth)

  local buf = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].filetype = "markdown"
  vim.bo[buf].modifiable = false

  local width = math.min(responseWidth + 4, vim.o.columns - 4)
  local height = math.min(math.max(#lines, 1) + 1, vim.o.lines - 4)

  vim.api.nvim_open_win(buf, true, {
    relative = "editor",
    width = width,
    height = height,
    row = math.floor((vim.o.lines - height) / 2),
    col = math.floor((vim.o.columns - width) / 2),
    style = "minimal",
    border = "rounded",
    title = " armin ",
    title_pos = "center",
  })
  vim.keymap.set("n", "q", "<cmd>close<CR>", { buffer = buf, nowait = true })
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
