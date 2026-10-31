// The header: the project, the model picker with prices, the
// OpenRouter balance, warnings, and the context gauge.

import { $, esc, api } from "./api.js";

export async function refreshInfo() {
  const info = await api("/api/info");
  $("root").textContent = info.root;
  const sel = $("model");
  const models = info.models || [ { name: info.model, model: info.label } ];
  const sugg   = (info.hosted && info.suggested) || [];
  const key    = models.map((m) => m.name).join(",") + "|" + sugg.map((m) => m.id).join(",");
  if (sel.dataset.models !== key) {
    const option = (value, label) => `<option value = "${ esc(value) }">${ esc(label) }</option>`;
    const mine   = models.map((m) => {
      let label = m.model && m.model !== m.name ? `${ m.name } — ${ m.model }` : m.name;
      label += m.cost ? ` · ${ costLabel(m.cost) }` : m.model && m.model.includes("/") ? " · hosted" : " · local, free";
      return option(m.name, label);
    }).join("");
    const known  = new Set(models.map((m) => m.name));
    const hosted = sugg.filter((m) => !known.has(info.hosted + ":" + m.id))
      .map((m) => option(info.hosted + ":" + m.id, `${ m.id } · ${ costLabel(m) }`)).join("");
    sel.innerHTML = `<optgroup label = "configured">${ mine }</optgroup>`
      + (hosted ? `<optgroup label = "suggested on OpenRouter (uses ${ esc(info.hosted) }'s key)">${ hosted }</optgroup>` : "");
    sel.dataset.models = key;
  }
  sel.value = info.model;

  const credits = $("credits");
  const b       = info.balance;
  if (b) {
    const left = b.credits - b.used;
    credits.textContent = `OpenRouter $${ left.toFixed(2) } left · today $${ (b.today || 0).toFixed(2) }`;
    credits.title       = b.key_limit ? `this key: $${ b.key_left.toFixed(2) } of its $${ b.key_limit.toFixed(2) } cap left` : "";
    credits.classList.toggle("low", left < 1);
  } else {
    credits.textContent = "";
  }
  $("warning").textContent = info.warning ? "⚠ " + info.warning : "";
  showContext(info.tokens || info.approx, info.window, info.tier, !info.tokens);
}

// costLabel is a hosted model's price per 1M tokens in / out, and a
// deep session (~1.2M in, 30k out, as armin runs one).
const costLabel = (c) => !c.in && !c.out ? "free"
  : `$${ c.in.toFixed(2) } / $${ c.out.toFixed(2) } per 1M · ~$${ c.session.toFixed(2) } a session`;

// kTokens renders a token count compactly: 950, 8.4k, 96k.
export const kTokens = (n) => n < 1000 ? String(n) : n < 10000 ? (n / 1000).toFixed(1) + "k" : Math.round(n / 1000) + "k";

// showContext fills the header gauge: used / window, the share, and
// the tier; yellow past half, red where compaction starts (75%).
export function showContext(used, window, tier, estimate) {
  const pct  = window ? Math.min(100, Math.round(used * 100 / window)) : 0;
  const mark = estimate ? "~" : "";
  const bar  = $("meter");

  bar.style.width      = pct + "%";
  bar.style.background = pct >= 75 ? "var(--delt)" : pct >= 50 ? "#d4a017" : "var(--accent)";

  $("ctx").textContent = window
    ? `${ mark }${ kTokens(used) } / ${ kTokens(window) } · ${ pct }%${ tier ? " · " + tier : "" }`
    : `${ mark }${ kTokens(used) } tokens`;
}
