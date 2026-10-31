// Talking to armin: $ for elements, esc for text going into HTML, and
// api for the JSON endpoints.

export const $ = (id) => document.getElementById(id);

const escapes = {
  "&":  "&amp;",
  "<":  "&lt;",
  ">":  "&gt;",
  "\"": "&quot;",
  "'":  "&#39;",
};

export const esc = (s) => s.replace(/[&<>"']/g, (c) => escapes[c]);

export async function api(path, body) {
  const opts = body === undefined ? {} : {
    method:  "POST",
    headers: { "Content-Type": "application/json" },
    body:    JSON.stringify(body),
  };

  const res = await fetch(path, opts);
  if (!res.ok) {
    throw new Error((await res.text()).trim() || res.statusText);
  }

  return res.headers.get("Content-Type")?.includes("json") ? res.json() : res.text();
}
