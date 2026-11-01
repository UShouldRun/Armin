// The header: the project, the model picker with prices, the
// OpenRouter balance, warnings, and the context gauge.

import { $, esc, api } from "./api.js";

// option is one <option> of the model picker.
const option = (value, label) => {
  return `<option value = "${ esc(value) }">${ esc(label) }</option>`;
};

// modelLabel says what a configured model is and what it costs.
const modelLabel = (m) => {
  let label = m.name;
  if (m.model && m.model !== m.name) {
    label = `${ m.name }: ${ m.model }`;
  }

  if (m.cost) {
    return label + ` · ${ costLabel(m.cost) }`;
  }

  if (m.model && m.model.includes("/")) {
    return label + " · hosted";
  }

  return label + " · local, free";
};

// fillModels rebuilds the picker: the configured models, then the
// suggested hosted ones not configured yet.
const fillModels = (sel, info, models, sugg) => {
  const mine   = models.map((m) => option(m.name, modelLabel(m))).join("");
  const known  = new Set(models.map((m) => m.name));
  const hosted = sugg
    .filter((m) => !known.has(info.hosted + ":" + m.id))
    .map((m) => {
      return option(info.hosted + ":" + m.id, `${ m.id } · ${ costLabel(m) }`);
    })
    .join("");

  let html = `<optgroup label = "configured">${ mine }</optgroup>`;
  if (hosted) {
    const label = `suggested on OpenRouter (uses ${ esc(info.hosted) }'s key)`;
    html += `<optgroup label = "${ label }">${ hosted }</optgroup>`;
  }

  sel.innerHTML = html;
};

// showBalance fills the OpenRouter credit readout.
const showBalance = (b) => {
  const credits = $("credits");
  if (!b) {
    credits.textContent = "";
    return;
  }

  const left  = b.credits - b.used;
  const today = (b.today || 0).toFixed(2);
  credits.textContent = `OpenRouter $${ left.toFixed(2) } left` +
    ` · today $${ today }`;
  credits.title       = "";
  if (b.key_limit) {
    const of = `of its $${ b.key_limit.toFixed(2) } cap left`;
    credits.title = `this key: $${ b.key_left.toFixed(2) } ${ of }`;
  }

  credits.classList.toggle("low", left < 1);
};

export async function refreshInfo() {
  const info = await api("/api/info");

  // the tree's root path bar is the only place the root shows now:
  // the header dropped its copy
  document.querySelectorAll("[data-root]").forEach((el) => {
    el.textContent = info.root;
    el.title       = "project root: " + info.root;
  });

  const sel    = $("model");
  const models = info.models || [ { name: info.model, model: info.label } ];
  const sugg   = (info.hosted && info.suggested) || [];
  const key    = models.map((m) => m.name).join(",") + "|" +
    sugg.map((m) => m.id).join(",");
  if (sel.dataset.models !== key) {
    fillModels(sel, info, models, sugg);
    sel.dataset.models = key;
  }

  sel.value = info.model;
  showBalance(info.balance);
  $("warning").textContent = info.warning ? "⚠ " + info.warning : "";
  showContext(info.tokens || info.approx, info.window, !info.tokens);
}

// costLabel is a hosted model's price per 1M tokens in / out, and a
// deep session (~1.2M in, 30k out, as armin runs one).
const costLabel = (c) => {
  if (!c.in && !c.out) {
    return "free";
  }

  const price = `$${ c.in.toFixed(2) } / $${ c.out.toFixed(2) } per 1M`;

  return price + ` · ~$${ c.session.toFixed(2) } a session`;
};

// kTokens renders a token count compactly: 950, 8.4k, 96k.
export const kTokens = (n) => {
  if (n < 1000) {
    return String(n);
  }

  if (n < 10000) {
    return (n / 1000).toFixed(1) + "k";
  }

  return Math.round(n / 1000) + "k";
};

// showContext fills the composer's context readout — used / window
// and the share — formatted like the REPL's prompt gauge, colored the
// same way: dim normally, yellow past half, red where compaction
// starts (75%).
export function showContext(used, window, estimate) {
  const pct  = window ? Math.min(100, Math.round(used * 100 / window)) : 0;
  const mark = estimate ? "~" : "";
  const text = $("ctx");

  text.classList.remove("hot", "full");
  if (pct >= 75) {
    text.classList.add("full");

  } else if (pct >= 50) {
    text.classList.add("hot");
  }

  text.textContent = window
    ? `${ mark }${ kTokens(used) }/${ kTokens(window) } ${ pct }%`
    : `${ mark }${ kTokens(used) } tokens`;
}
