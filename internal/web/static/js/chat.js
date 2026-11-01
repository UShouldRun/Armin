// The conversation: adding messages, approvals, the status bar, and
// sending a message and reading its event stream back.

import { $, esc, api }                          from "./api.js";
import { state }                                from "./state.js";
import { markdown }                             from "./markdown.js";
import { refreshInfo, kTokens, showContext }    from "./header.js";
import {
  loadFiles, changers, filesChanged, parseChange, showChange,
} from "./viewer.js";

// Messages typed while a turn runs, sent when it ends — like the
// REPL's composer queue.
const queued = [];

export function hold(text) {
  queued.push(text);
  add("notice", `queued: ${ esc(text) }`);
}

export function add(cls, html) {
  const log = $("log");
  log.querySelector(".empty")?.remove();

  const el = document.createElement("div");
  el.className = cls;
  el.innerHTML = html;
  log.appendChild(el);
  log.scrollTop = log.scrollHeight;

  return el;
}

// toolLine is a tool call's line: its name in bold, then the rest.
const toolLine = (line) => {
  const cut  = line.indexOf("  ");
  const name = cut < 0 ? line : line.slice(0, cut);
  const rest = cut < 0 ? "" : line.slice(cut + 2);
  const dot  = `<span class = "dot">●</span>`;

  return `${ dot } <b>${ esc(name) }</b>  ${ esc(rest) }`;
};

// What each kind of approval asks, and its buttons.
const approvalText = {
  edit: {
    what: "wants to change a file:",
    yes:  "Apply",
    done: "applied",
  },
  run_program: {
    what: "wants to run:",
    yes:  "Run",
    done: "ran",
  },
  "run_program (privileged)": {
    what: "wants to run a privileged command:",
    yes:  "Run",
    done: "ran",
  },
  request_access: {
    what: "wants to read outside the project:",
    yes:  "Grant",
    done: "granted",
  },
};

// diffLine colors a preview's added and removed lines.
const diffLine = (l) => {
  if (l.startsWith("+ ")) {
    return `<span class = "add">${ esc(l) }</span>`;
  }

  if (l.startsWith("- ")) {
    return `<span class = "del">${ esc(l) }</span>`;
  }

  return esc(l);
};

function approvalBox(ev) {
  const change = parseChange(ev.preview);
  const ask    = approvalText[ev.tool] || approvalText.edit;

  let body = `<pre>${ ev.preview.split("\n").map(diffLine).join("\n") }</pre>`;
  if (change) {
    const first = esc(ev.preview.split("\n")[0]);
    body = `<div class = "view">${ first } — shown in the viewer ` +
      `(click to show again)</div>`;
    showChange(change);
  }

  const box = add(
    "approve",
    `<div><b>${ esc(ev.tool) }</b> ${ ask.what }</div>
    ${ body }
    <button data-ok = "1">${ ask.yes }</button>
    <button data-ok = "0">Decline</button>
    <input placeholder = "or tell the model what to do instead">`,
  );
  box.addEventListener("click", async (e) => {
    if (change && e.target.classList.contains("view")) {
      showChange(change);
      return;
    }

    if (!e.target.dataset.ok) {
      return;
    }

    const ok       = e.target.dataset.ok === "1";
    const feedback = box.querySelector("input").value;
    const yes      = ok && !feedback;
    await api("/api/approve", { id: ev.id, ok: yes, feedback });
    box.querySelectorAll("button, input").forEach((b) => {
      b.disabled = true;
    });

    const said = yes ? ask.done : "declined";
    box.insertAdjacentHTML("beforeend", `<div class = "tool">${ said }</div>`);
  });
}

// ---------- status ----------

// What the turn is doing, for the status bar: the phase, since when,
// the last sign of life from the model and from armin itself.
const stallAfter = 20000; // ms of model silence before it says so
const lostAfter  = 15000; // ms without armin's 5 s heartbeat

const st = {
  phase: "",
  since: 0,
  last:  0,
  heard: 0,
  watch: false,
  count: 0,
};

function since(t) {
  const s = Math.round((Date.now() - t) / 1000);
  if (s < 60) {
    return `${ s }s`;
  }

  return `${ Math.floor(s / 60) }m${ String(s % 60).padStart(2, "0") }s`;
}

function setPhase(phase, watch) {
  const now = Date.now();
  Object.assign(st, {
    phase,
    since: now,
    last:  now,
    heard: now,
    watch,
    count: 0,
  });
  renderStatus();
}

function renderStatus() {
  const bar = $("status");
  const now = Date.now();
  let text  = st.phase;
  if (st.count) {
    text += ` · ${ st.count } tokens`;
  }

  text += " · " + since(st.since);
  if (st.phase === "waiting for the model" && now - st.since > 5000) {
    text += " (it may be loading or busy)";
  }

  let cls = "";
  if (st.watch && now - st.last > stallAfter) {
    text += " · nothing from the model for " + since(st.last);
    cls   = "stall";
  }

  if (now - st.heard > lostAfter) {
    text = `no word from armin for ${ since(st.heard) } · the ` +
      "connection may be lost (is armin still running?)";
    cls  = "lost";
  }

  bar.textContent = text;
  bar.className   = cls;
}

export async function send(text) {
  state.busy = true;
  $("send").disabled = true;
  add("msg user", esc(text));

  let answer   = "";
  let thoughts = "";
  let think    = null;
  let msg      = null;
  let pending  = false;
  let lastCall = null;

  setPhase("waiting for the model", true);
  $("status").hidden = false;
  const ticker = setInterval(renderStatus, 500);

  const paint = () => {
    pending = false;
    if (msg) {
      msg.innerHTML = markdown(answer);
    }

    if (think) {
      think.querySelector("div").textContent = thoughts;
    }

    $("log").scrollTop = $("log").scrollHeight;
  };

  const schedule = () => {
    if (!pending) {
      pending = true;
      requestAnimationFrame(paint);
    }
  };

  const handle = (kind, data) => {
    st.heard = Date.now();
    switch (kind) {
    case "ping":
      break;

    case "progress": {
      if (!st.phase.startsWith("reading the prompt")) {
        setPhase("", true);
      }

      const done = `${ kTokens(data.done) }/${ kTokens(data.total) }`;
      st.phase = `reading the prompt ${ done }`;
      st.last  = Date.now();
      break;
    }

    case "reasoning":
      if (st.phase !== "thinking") {
        setPhase("thinking", true);
      }

      st.count++;
      st.last = Date.now();
      if (!think) {
        think = document.createElement("details");
        think.className = "think";
        think.innerHTML = "<summary>thinking</summary><div></div>";
        $("log").querySelector(".empty")?.remove();
        $("log").appendChild(think);
      }

      thoughts += data;
      schedule();
      break;

    case "token":
      if (st.phase !== "writing") {
        setPhase("writing", true);
      }

      st.count++;
      st.last = Date.now();
      if (!msg) {
        msg = add("msg md", "");
      }

      answer += data;
      schedule();
      break;

    case "tool":
      add("tool", toolLine(data.line || data.name));

      // draw what streamed before this call, then start anew
      paint();
      msg      = null;
      answer   = "";
      think    = null;
      thoughts = "";
      lastCall = data;
      setPhase("running " + (data.args?.binary || data.name), false);
      break;

    case "result": {
      const failed = data.startsWith("error:");
      setPhase("waiting for the model", true);
      add(failed ? "tool err" : "tool result", "└ " + esc(data));
      if (!failed && lastCall && changers.includes(lastCall.name)) {
        filesChanged(lastCall);
      }

      break;
    }

    case "notice":
      add("notice", "context: " + esc(data));
      break;

    case "usage":
      showContext(data.tokens, data.window, false);
      break;

    case "approve":
      setPhase("waiting for your approval", false);
      approvalBox(data);
      break;

    case "error": {
      // {message, trace} from the server, or a plain string (the
      // fetch failed before the stream started)
      let m = data;
      if (typeof data === "string") {
        m = { message: data, trace: "" };
      }

      const div = add("msg err", esc(m.message));
      if (m.trace) {
        const d = document.createElement("details");
        d.className = "trace";
        d.innerHTML = "<summary>trace</summary>";

        const pre = document.createElement("pre");
        pre.textContent = m.trace;
        d.appendChild(pre);
        div.appendChild(d);
      }

      break;
    }

    case "done":
      if (!msg && data) {
        msg = add("msg md", "");
      }

      if (data) {
        answer = data;
      }

      paint();
      break;
    }
  };

  try {
    const res = await fetch("/api/chat", {
      method:  "POST",
      headers: { "Content-Type": "application/json" },
      body:    JSON.stringify({ message: text }),
    });
    if (!res.ok) {
      throw new Error((await res.text()).trim());
    }

    const reader = res.body.getReader();
    const dec    = new TextDecoder();
    let buf      = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) {
        break;
      }

      buf += dec.decode(value, { stream: true });
      let cut = buf.indexOf("\n\n");
      while (cut >= 0) {
        const block = buf.slice(0, cut);
        buf = buf.slice(cut + 2);

        const kind = block.match(/^event: (.*)$/m)?.[1];
        const data = block.match(/^data: (.*)$/m)?.[1];
        if (kind && data !== undefined) {
          handle(kind, JSON.parse(data));
        }

        cut = buf.indexOf("\n\n");
      }
    }

  } catch (e) {
    console.error("[ERROR]: send - ", e);
    add("msg err", esc(e.message));

  } finally {
    clearInterval(ticker);
    $("status").hidden = true;
    state.busy = false;
    $("send").disabled = false;
    refreshInfo();
    loadFiles();

    const next = queued.shift();
    if (next) {
      send(next);
    }
  }
}

// A resumed session shows what was said before.
export async function loadHistory() {
  try {
    const h = await api("/api/history");
    for (const e of h.entries) {
      if (e.role === "user") {
        add("msg user", esc(e.text));

      } else if (e.role === "assistant") {
        add("msg md", markdown(e.text));

      } else {
        add("tool", toolLine(e.text));
      }
    }

    if (h.entries.length) {
      const title = esc(h.title || "untitled");
      const id    = esc(h.id.split("-")[0]);
      add("notice", `resumed session: ${ title } (${ id })`);
    }

    $("log").scrollTop = $("log").scrollHeight;

  } catch (e) {
    console.error("[ERROR]: loadHistory - ", e);
  }
}
