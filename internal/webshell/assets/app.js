/* SHEYTAN-LA adaptive web shell — the application layer.
   Every view renders REAL core data served on the same origin; nothing
   here is decorative. The core is the one authority: this script only
   reads what the core publishes and calls the core's documented routes. */

"use strict";

/* ------------------------------------------------------------------ */
/* The ONE workspace registry: ids, order and grouping shared with the */
/* native mirror shell (ui/gogui) and the Go registry in webshell.go.  */
/* ------------------------------------------------------------------ */

const ICONS = {
  chat: "M4 5h16v10H9l-5 4V5zm2 2v8l2.4-2H18V7H6z",
  agent: "M12 3l2.2 4.9L19 9l-4.8 1.1L12 15l-2.2-4.9L5 9l4.8-1.1L12 3zm6 10l1.2 2.4L22 16l-2.8.6L18 19l-1.2-2.4L14 16l2.8-.6L18 13zM5 14l.9 1.8L8 16l-2.1.4L5 18l-.9-1.6L2 16l2.1-.2L5 14z",
  workspace: "M3 5h8l2 2h8v12H3V5zm2 4v8h14V9H5z",
  lab: "M9 3h6v5l4 9a2 2 0 0 1-1.8 3H6.8A2 2 0 0 1 5 17l4-9V3zm2 2v3.6L9.1 13h5.8L13 8.6V5h-2z",
  automation: "M4 6h16v3H4V6zm0 5h10v3H4v-3zm0 5h16v3H4v-3zm13-8l3 1.5L17 11V8z",
  system: "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-1 5h2v6h-2V7zm0 8h2v2h-2v-2z",
  settings: "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8zm9 4l-2.1-.5-.6-1.5 1.1-1.8-1.6-1.6-1.8 1.1-1.5-.6L14 5h-2.3l-.5 2.1-1.5.6L7.9 6.6 6.3 8.2l1.1 1.8-.6 1.5L5 12v2.3l2.1.5.6 1.5-1.1 1.8 1.6 1.6 1.8-1.1 1.5.6.5 2.1H14l.5-2.1 1.5-.6 1.8 1.1 1.6-1.6-1.1-1.8.6-1.5L21 14v-2z",
  studio: "M12 2l9 5v10l-9 5-9-5V7l9-5zm0 2.3L5 8v8l7 3.9 7-3.9V8l-7-3.7z",
  engines: "M7 4h10v3h3v10h-3v3H7v-3H4V7h3V4zm2 2v3H6v6h3v3h6v-3h3V9h-3V6H9z",
  history: "M13 3a9 9 0 1 1-8.5 12h2.2A7 7 0 1 0 13 5v3H8l5 5 5-5h-3V3h-2zm-2 7v6h-2v-6h2z",
  models: "M4 6h16v4H4V6zm0 8h16v4H4v-4zm3-6v0m0 10v0m3-10v0m0 10v0",
  tools: "M21 7l-4-4-2 2 4 4 2-2zM11 9l2 2-7 7-2 1 1-2 7-7zM3 21h8v-1H3v1z",
  logs: "M5 3h14v18H5V3zm2 2v2h10V5H7zm0 4v2h10V9H7zm0 4v2h6v-2H7z"
};

const REGISTRY = [
  { group: "primary", id: "chat", title: "Chat", key: "1" },
  { group: "primary", id: "agent", title: "Agent", key: "2" },
  { group: "primary", id: "workspace", title: "Workspace", key: "3" },
  { group: "primary", id: "lab", title: "Coding Lab", key: "4" },
  { group: "primary", id: "automation", title: "Automation", key: "5" },
  { group: "primary", id: "system", title: "System", key: "6" },
  { group: "primary", id: "settings", title: "Settings", key: "7" },
  { group: "primary", id: "studio", title: "3D Studio", key: "8" },
  { group: "primary", id: "engines", title: "Engines", key: "9" },
  { group: "utility", id: "history", title: "History", key: "" },
  { group: "utility", id: "models", title: "Models", key: "" },
  { group: "utility", id: "tools", title: "My Tools", key: "" },
  { group: "utility", id: "logs", title: "Logs", key: "" }
];

/* The long-running download progress route (internal/downloader). */
const UpdateCheckPath = "/api/engine/update/check";

/* ------------------------------------------------------------------ */
/* Small DOM + formatting helpers.                                     */
/* ------------------------------------------------------------------ */

const $ = (sel) => document.querySelector(sel);

function el(tag, cls, text) {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function icon(name) {
  const holder = document.createElement("span");
  /* innerHTML is CSP-safe here: the markup carries no script content, and
     the HTML parser routes <svg> into SVG foreign content automatically. */
  holder.innerHTML =
    '<svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor"' +
    ' stroke-width="1.6" stroke-linejoin="round"><path d="' +
    (ICONS[name] || ICONS.system) +
    '"></path></svg>';
  const svg = holder.firstChild;
  svg.style.width = "100%";
  svg.style.height = "100%";
  return svg;
}

function humanBytes(n) {
  if (!Number.isFinite(n) || n < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u += 1;
  }
  const digits = v >= 100 || u === 0 ? 0 : v >= 10 ? 1 : 2;
  return v.toFixed(digits) + " " + units[u];
}

function when(t) {
  if (!t) return "";
  const d = new Date(t);
  if (Number.isNaN(d.getTime())) return String(t);
  return d.toLocaleString();
}

/* ------------------------------------------------------------------ */
/* Notifications: bounded toasts.                                      */
/* ------------------------------------------------------------------ */

const notes = [];
const MAX_NOTES = 5;

function notify(kind, text) {
  const host = $("#notifications");
  if (!host) return;
  while (notes.length >= MAX_NOTES) {
    const dropped = notes.shift();
    dropped.remove();
  }
  const t = el("div", "toast " + kind);
  const body = el("span", "grow", text);
  const close = el("button", "close");
  close.type = "button";
  close.textContent = "\u00d7";
  close.setAttribute("aria-label", "Dismiss");
  close.addEventListener("click", () => {
    const i = notes.indexOf(t);
    if (i >= 0) notes.splice(i, 1);
    t.remove();
  });
  t.append(body, close);
  notes.push(t);
  host.appendChild(t);
  window.setTimeout(() => close.click(), 8000);
}

/* ------------------------------------------------------------------ */
/* API layer: same-origin JSON. Errors surface as notifications.       */
/* ------------------------------------------------------------------ */

async function api(path, options) {
  const opts = Object.assign({ headers: { "Accept": "application/json" } }, options);
  if (opts.body && typeof opts.body !== "string") {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(opts.body);
  }
  let res;
  try {
    res = await fetch(path, opts);
  } catch (err) {
    notify("danger", "core unreachable: " + err.message);
    throw err;
  }
  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch (err) {
    data = { raw: text };
  }
  if (!res.ok) {
    const why = data && (data.error || data.message) ? String(data.error || data.message) : res.status + " " + res.statusText;
    notify("danger", path + " refused: " + why);
    throw new Error(why);
  }
  return data;
}

/* Defensive key/value rendering for endpoints whose shape may grow. */
function renderKV(host, data, skipKeys) {
  host.textContent = "";
  const skip = new Set(skipKeys || []);
  if (data === null || data === undefined) {
    host.appendChild(el("span", "muted", "no data"));
    return;
  }
  if (typeof data !== "object") {
    host.appendChild(el("span", "v", String(data)));
    return;
  }
  for (const [k, v] of Object.entries(data)) {
    if (skip.has(k)) continue;
    const row = el("div", "kv");
    row.appendChild(el("span", "k", k));
    const val = el("span", "v mono", typeof v === "object" ? JSON.stringify(v) : String(v));
    row.appendChild(val);
    host.appendChild(row);
  }
}

function badge(text, kind) {
  const b = el("span", "badge" + (kind ? " " + kind : ""), text);
  return b;
}

/* ------------------------------------------------------------------ */
/* Core status dot + status bar.                                       */
/* ------------------------------------------------------------------ */

let healthTimer = null;

async function refreshHealth() {
  const dot = $("#core-dot");
  const label = $("#core-state");
  try {
    const h = await api("/api/health");
    if (dot) dot.className = "status-dot ok";
    if (label) label.textContent = "core " + (h && h.status ? h.status : "ok");
    setStatusRight(h);
  } catch (err) {
    if (dot) dot.className = "status-dot bad";
    if (label) label.textContent = "core unreachable";
  }
}

function setStatusRight(h) {
  const right = $("#status-right");
  if (!right) return;
  if (h && h.version) right.textContent = "v" + h.version + (h.uptime ? " · up " + h.uptime : "");
}

function startHealth() {
  stopHealth();
  refreshHealth();
  healthTimer = window.setInterval(refreshHealth, 10000);
}

function stopHealth() {
  if (healthTimer !== null) {
    window.clearInterval(healthTimer);
    healthTimer = null;
  }
}

/* ------------------------------------------------------------------ */
/* Download-progress polling (engine update surface).                 */
/* Shared lifecycle contract: polling runs ONLY while visible and only */
/* while the surface is mounted.                                       */
/* ------------------------------------------------------------------ */

let dlTimer = null;
let dlPhase = "idle";

function renderDownload(d) {
  const bar = $("#dl-bar");
  const wrap = $("#dl-wrap");
  if (!bar || !wrap) return;
  const phase = d && d.phase ? String(d.phase) : "idle";
  const done = d && Number(d.bytesDone) || 0;
  const total = d && Number(d.bytesTotal) || 0;
  const speed = d && Number(d.bytesPerSec) || 0;
  const running = phase !== "idle" && phase !== "done" && phase !== "error";
  const pct = total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0;
  bar.style.width = pct + "%";
  wrap.setAttribute("aria-valuenow", String(pct));
  dlPhase = phase;
  const phaseEl = $("#dl-phase");
  const bytesEl = $("#dl-bytes");
  const speedEl = $("#dl-speed");
  const statusEl = $("#dl-status");
  if (phaseEl) phaseEl.textContent = phase;
  if (bytesEl) bytesEl.textContent = humanBytes(done) + " / " + humanBytes(total);
  if (speedEl) speedEl.textContent = humanBytes(speed) + "/s";
  if (statusEl) {
    statusEl.textContent = d && d.message ? d.message : running ? "downloading" : phase;
  }
  return running;
}

async function pollDownload() {
  try {
    const d = await api(UpdateCheckPath);
    renderDownload(d);
  } catch (err) {
    renderDownload(null);
  }
}

function startPolling() {
  if (dlTimer !== null) return;
  pollDownload();
  dlTimer = window.setInterval(pollDownload, 1500);
}

function stopPolling() {
  if (dlTimer !== null) {
    window.clearInterval(dlTimer);
    dlTimer = null;
  }
}

document.addEventListener("visibilitychange", () => {
  if (document.hidden) {
    stopPolling();
    stopHealth();
  } else {
    startPolling();
  }
});

/* ------------------------------------------------------------------ */
/* Module views — each loader reads REAL core routes.                  */
/* ------------------------------------------------------------------ */

const loaders = {};
const activeTimers = [];

function clearLoaders() {
  while (activeTimers.length) window.clearInterval(activeTimers.pop());
}

function every(ms, fn) {
  fn();
  activeTimers.push(window.setInterval(fn, ms));
}

/* --- chat ----------------------------------------------------------- */

let currentSession = "";

loaders.chat = () => {
  every(5000, loadSessions);
};

async function loadSessions() {
  const list = $("#chat-session-list");
  if (!list) return;
  const data = await api("/api/sessions");
  const sessions = Array.isArray(data) ? data : data && data.sessions ? data.sessions : [];
  list.textContent = "";
  if (!sessions.length) {
    list.appendChild(el("li", "muted", "no sessions yet"));
    return;
  }
  for (const s of sessions) {
    const li = el("li");
    li.setAttribute("role", "button");
    li.setAttribute("tabindex", "0");
    const main = el("div", "grow");
    main.appendChild(el("div", null, s.title || s.id));
    main.appendChild(el("div", "sub", (s.model || "") + (s.msgCount !== undefined ? " · " + s.msgCount + " messages" : "") + (s.updatedAt ? " · " + when(s.updatedAt) : "")));
    li.appendChild(main);
    if (s.id === currentSession) li.appendChild(badge("open", "accent"));
    const open = () => openSession(s.id);
    li.addEventListener("click", open);
    li.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        open();
      }
    });
    list.appendChild(li);
  }
}

async function openSession(id) {
  currentSession = id;
  const log = $("#chat-log");
  const titleEl = $("#chat-title");
  const badgeEl = $("#chat-session-badge");
  if (!log) return;
  try {
    const s = await api("/api/sessions/" + encodeURIComponent(id));
    if (titleEl) titleEl.textContent = s.title || id;
    if (badgeEl) {
      badgeEl.textContent = s.model || "";
      badgeEl.className = "badge accent";
    }
    log.textContent = "";
    const msgs = Array.isArray(s.messages) ? s.messages : [];
    if (!msgs.length) log.appendChild(el("span", "muted", "this session has no messages yet"));
    for (const m of msgs) {
      const who = m.role === "user" ? "user" : "core";
      const div = el("div", "msg " + who);
      div.appendChild(el("span", "who", m.role || who));
      div.appendChild(el("span", null, m.content || ""));
      log.appendChild(div);
    }
    log.scrollTop = log.scrollHeight;
    loadSessions();
  } catch (err) {
    notify("danger", "session open failed: " + err.message);
  }
}

async function setupComposer() {
  const form = $("#composer");
  const input = $("#composer-input");
  const newBtn = $("#chat-new");
  if (!form || !input) return;
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text) return;
    input.value = "";
    try {
      if (!currentSession) {
        const created = await api("/api/sessions", { method: "POST", body: {} });
        currentSession = created && (created.id || created.ID) ? created.id : "";
        if (!currentSession) throw new Error("the core refused to create a session");
      }
      const log = $("#chat-log");
      if (log) {
        const div = el("div", "msg user");
        div.appendChild(el("span", "who", "user"));
        div.appendChild(el("span", null, text));
        log.appendChild(div);
        log.scrollTop = log.scrollHeight;
      }
      const res = await api("/api/run", { method: "POST", body: { sessionId: currentSession, message: text } });
      if (res && res.sessionId) currentSession = res.sessionId;
      notify("ok", "run " + (res && res.runId ? res.runId : "") + " " + (res && res.state ? res.state : "accepted"));
      let polls = 0;
      const watch = window.setInterval(async () => {
        polls += 1;
        try {
          await openSessionQuiet(currentSession);
        } catch (err) { /* transient */ }
        if (polls >= 30) window.clearInterval(watch);
      }, 2000);
      activeTimers.push(watch);
    } catch (err) {
      notify("danger", "send failed: " + err.message);
    }
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      form.requestSubmit();
    }
  });
  if (newBtn) {
    newBtn.addEventListener("click", async () => {
      try {
        const created = await api("/api/sessions", { method: "POST", body: {} });
        currentSession = created && created.id ? created.id : "";
        const log = $("#chat-log");
        if (log) log.textContent = "";
        const titleEl = $("#chat-title");
        if (titleEl) titleEl.textContent = "Conversation";
        notify("ok", "new session created");
        loadSessions();
      } catch (err) {
        notify("danger", "session create failed: " + err.message);
      }
    });
  }
}

async function openSessionQuiet(id) {
  if (!id) return;
  const s = await api("/api/sessions/" + encodeURIComponent(id));
  const log = $("#chat-log");
  if (!log) return;
  const msgs = Array.isArray(s.messages) ? s.messages : [];
  log.textContent = "";
  for (const m of msgs) {
    const who = m.role === "user" ? "user" : "core";
    const div = el("div", "msg " + who);
    div.appendChild(el("span", "who", m.role || who));
    div.appendChild(el("span", null, m.content || ""));
    log.appendChild(div);
  }
  log.scrollTop = log.scrollHeight;
}

/* --- agent ----------------------------------------------------------- */

loaders.agent = () => {
  bindOnce("#agent-refresh", loadGoals);
  bindOnce("#run-paused-btn", loadPaused);
  bindOnce("#run-abort-btn", async () => {
    if (!currentSession) {
      notify("warn", "no open session to abort");
      return;
    }
    await api("/api/abort", { method: "POST", body: { sessionId: currentSession } });
    notify("ok", "abort requested for " + currentSession);
  });
  bindOnce("#goal-form", async (e) => {
    e.preventDefault();
    const input = $("#goal-input");
    const text = input && input.value.trim();
    if (!text) return;
    await api("/api/goals", { method: "POST", body: { title: text } });
    if (input) input.value = "";
    loadGoals();
  });
  every(8000, loadGoals);
};

async function loadGoals() {
  const list = $("#goal-list");
  if (!list) return;
  const data = await api("/api/goals");
  const goals = Array.isArray(data) ? data : data && data.goals ? data.goals : [];
  list.textContent = "";
  if (!goals.length) {
    list.appendChild(el("li", "muted", "no goals recorded"));
    return;
  }
  for (const g of goals) {
    const li = el("li");
    li.appendChild(el("div", "grow", g.title || g.name || g.id || "goal"));
    li.appendChild(badge(g.status || g.state || "open", g.status === "done" || g.state === "done" ? "ok" : ""));
    list.appendChild(li);
  }
}

async function loadPaused() {
  const host = $("#agent-run-state");
  if (!host) return;
  const data = await api("/api/run/paused");
  const runs = Array.isArray(data) ? data : data && data.runs ? data.runs : [];
  host.textContent = "";
  host.appendChild(el("div", "kv")).append(el("span", "k", "paused runs"), el("span", "v", String(runs.length)));
  for (const r of runs.slice(0, 5)) {
    host.appendChild(el("div", "sub mono", (r.runId || "") + " " + (r.sessionId || "")));
  }
}

/* --- workspace -------------------------------------------------------- */

loaders.workspace = () => {
  bindOnce("#ws-refresh", loadRepoIndex);
  bindOnce("#ws-search-form", async (e) => {
    e.preventDefault();
    const input = $("#ws-search-input");
    const q = input && input.value.trim();
    const out = $("#ws-results");
    if (!out) return;
    out.textContent = "";
    if (!q) return;
    const data = await api("/api/repo/search?q=" + encodeURIComponent(q));
    const results = Array.isArray(data) ? data : data && data.results ? data.results : [];
    if (!results.length) out.appendChild(el("li", "muted", "no matches"));
    for (const r of results.slice(0, 20)) {
      const li = el("li");
      li.appendChild(el("div", "grow mono", r.path || r.file || JSON.stringify(r)));
      if (r.score !== undefined) li.appendChild(badge(String(r.score)));
      out.appendChild(li);
    }
  });
  every(20000, loadRepoIndex);
};

async function loadRepoIndex() {
  const host = $("#ws-index-state");
  if (!host) return;
  try {
    const data = await api("/api/repo/index");
    renderKV(host, data);
  } catch (err) {
    host.textContent = "";
    host.appendChild(el("span", "muted", "index unavailable"));
  }
}

/* --- lab --------------------------------------------------------------- */

loaders.lab = () => {
  bindOnce("#lab-refresh", loadLab);
  every(15000, loadLab);
};

async function loadLab() {
  const host = $("#lab-state");
  if (!host) return;
  const data = await api("/api/lab");
  host.textContent = "";
  const items = Array.isArray(data) ? data : data && data.experiments ? data.experiments : [];
  if (!items.length) {
    renderKV(host, data);
    return;
  }
  for (const it of items.slice(0, 12)) {
    const row = el("div", "kv");
    row.appendChild(el("span", "k", it.id || it.name || "experiment"));
    row.appendChild(el("span", "v", it.status || it.state || ""));
    host.appendChild(row);
  }
}

/* --- automation --------------------------------------------------------- */

loaders.automation = () => {
  bindOnce("#auto-refresh", loadAutomation);
  every(12000, loadAutomation);
};

async function loadAutomation() {
  const list = $("#automation-list");
  if (list) {
    const data = await api("/api/automation/tasks");
    const tasks = Array.isArray(data) ? data : data && data.tasks ? data.tasks : [];
    list.textContent = "";
    if (!tasks.length) list.appendChild(el("li", "muted", "no tasks"));
    for (const t of tasks.slice(0, 15)) {
      const li = el("li");
      const main = el("div", "grow");
      main.appendChild(el("div", null, t.title || t.name || t.id));
      main.appendChild(el("div", "sub", t.schedule || t.cron || t.id || ""));
      li.appendChild(main);
      const state = t.state || t.status || "";
      if (state) li.appendChild(badge(state, state === "running" ? "ok" : state === "failed" ? "danger" : ""));
      list.appendChild(li);
    }
  }
  const artifacts = $("#artifact-list");
  if (artifacts) {
    const data = await api("/api/automation/artifacts");
    const arts = Array.isArray(data) ? data : data && data.artifacts ? data.artifacts : [];
    artifacts.textContent = "";
    if (!arts.length) artifacts.appendChild(el("li", "muted", "no artifacts"));
    for (const a of arts.slice(0, 15)) {
      const li = el("li");
      li.appendChild(el("div", "grow", a.name || a.id || "artifact"));
      li.appendChild(el("span", "sub", a.createdAt ? when(a.createdAt) : ""));
      artifacts.appendChild(li);
    }
  }
}

/* --- system -------------------------------------------------------------- */

loaders.system = () => {
  bindOnce("#sys-refresh", loadSystem);
  bindOnce("#netcheck-btn", async () => {
    const host = $("#sys-netcheck");
    if (host) host.textContent = "";
    const data = await api("/api/netcheck", { method: "POST" });
    if (host) renderKV(host, data);
  });
  every(15000, loadSystem);
};

async function loadSystem() {
  const health = $("#sys-health");
  if (health) {
    try {
      renderKV(health, await api("/api/health"));
    } catch (err) {
      renderKV(health, { status: "unreachable" });
    }
  }
  const env = $("#sys-environment");
  if (env) {
    try {
      renderKV(env, await api("/api/environment"));
    } catch (err) {
      env.textContent = "";
      env.appendChild(el("span", "muted", "environment unavailable"));
    }
  }
  const perf = $("#sys-perf");
  if (perf) {
    try {
      renderKV(perf, await api("/api/perf"));
    } catch (err) {
      perf.textContent = "";
      perf.appendChild(el("span", "muted", "performance data unavailable"));
    }
  }
  const sys = $("#sys-netcheck");
  if (sys && !sys.childElementCount) sys.appendChild(el("span", "muted", "run a check to see connectivity truth"));
}

/* --- settings ------------------------------------------------------------- */

let settingsDoc = null;

loaders.settings = () => {
  bindOnce("#settings-reload", loadSettings);
  bindOnce("#settings-save", saveSettings);
  every(30000, loadSettings);
};

async function loadSettings() {
  const host = $("#settings-body");
  if (!host) return;
  settingsDoc = await api("/api/config");
  host.textContent = "";
  if (!settingsDoc || typeof settingsDoc !== "object") {
    host.appendChild(el("span", "muted", "the core published no configuration"));
    return;
  }
  for (const [k, v] of Object.entries(settingsDoc)) {
    const row = el("div", "inline-form");
    row.style.marginTop = "0";
    const label = el("label", null, k);
    label.setAttribute("for", "cfg-" + k);
    label.style.minWidth = "160px";
    label.style.color = "var(--fg-muted)";
    if (v !== null && typeof v === "object") {
      const pre = el("span", "mono", JSON.stringify(v));
      pre.className = "sub mono";
      row.appendChild(label);
      row.appendChild(pre);
    } else {
      const input = el("input");
      input.id = "cfg-" + k;
      input.dataset.key = k;
      input.value = v === null || v === undefined ? "" : String(v);
      input.readOnly = typeof v === "boolean";
      row.appendChild(label);
      row.appendChild(input);
    }
    host.appendChild(row);
  }
}

async function saveSettings() {
  if (!settingsDoc) return;
  const patch = {};
  for (const input of document.querySelectorAll("#settings-body input[data-key]")) {
    const key = input.dataset.key;
    const original = settingsDoc[key];
    if (typeof original === "number") {
      const n = Number(input.value);
      if (Number.isFinite(n) && n !== original) patch[key] = n;
    } else if (typeof original === "string") {
      if (input.value !== original) patch[key] = input.value;
    }
  }
  if (!Object.keys(patch).length) {
    notify("warn", "nothing changed");
    return;
  }
  await api("/api/config", { method: "PATCH", body: patch });
  notify("ok", "configuration updated (the core validated it)");
  loadSettings();
}

/* --- studio ----------------------------------------------------------------- */

loaders.studio = () => {
  bindOnce("#studio-start", async () => {
    await api("/api/studio/start", { method: "POST" });
    loadStudio();
  });
  bindOnce("#studio-stop", async () => {
    await api("/api/studio/stop", { method: "POST" });
    loadStudio();
  });
  every(10000, loadStudio);
};

async function loadStudio() {
  const host = $("#studio-state");
  if (!host) return;
  try {
    renderKV(host, await api("/api/studio/state"));
  } catch (err) {
    host.textContent = "";
    host.appendChild(el("span", "muted", "studio state unavailable"));
  }
}

/* --- engines ------------------------------------------------------------------ */

loaders.engines = () => {
  bindOnce("#engine-refresh", loadEngines);
  bindOnce("#engine-rediscover", async () => {
    await api("/api/engine/rediscover", { method: "POST" });
    notify("ok", "engine rediscovery requested");
    loadEngines();
  });
  bindOnce("#engine-update", async () => {
    const check = await api(UpdateCheckPath);
    renderDownload(check);
    const running = check && check.phase && check.phase !== "idle" && check.phase !== "done" && check.phase !== "error";
    if (running) {
      notify("warn", "an engine update is already in progress");
      return;
    }
    if (!window.confirm("Start an engine update from the official source now?")) return;
    await api("/api/engine/update", { method: "POST" });
    notify("ok", "engine update started — progress is on this surface");
    startPolling();
  });
  every(5000, loadEngines);
  pollDownload();
};

async function loadEngines() {
  const host = $("#engine-state");
  if (!host) return;
  try {
    const engine = await api("/api/engine");
    const llama = await api("/api/llama").catch(() => null);
    host.textContent = "";
    renderKV(host, Object.assign({}, engine, llama || {}));
  } catch (err) {
    host.textContent = "";
    host.appendChild(el("span", "muted", "engine status unavailable"));
  }
}

/* --- history -------------------------------------------------------------------- */

loaders.history = () => {
  bindOnce("#history-form", async (e) => {
    e.preventDefault();
    const input = $("#history-input");
    const q = input && input.value.trim();
    const out = $("#history-results");
    if (!out) return;
    out.textContent = "";
    if (!q) return;
    const data = await api("/api/history/search?limit=20&q=" + encodeURIComponent(q));
    const items = Array.isArray(data) ? data : data && data.results ? data.results : [];
    if (!items.length) out.appendChild(el("li", "muted", "no history matches"));
    for (const h of items.slice(0, 20)) {
      const li = el("li");
      const main = el("div", "grow");
      main.appendChild(el("div", null, h.title || h.summary || h.id || "record"));
      main.appendChild(el("div", "sub", (h.sessionId || "") + (h.at ? " · " + when(h.at) : "")));
      li.appendChild(main);
      out.appendChild(li);
    }
  });
};

/* --- models ------------------------------------------------------------------------ */

loaders.models = () => {
  bindOnce("#models-refresh", loadModels);
  every(20000, loadModels);
};

async function loadModels() {
  const list = $("#models-list");
  if (list) {
    const data = await api("/api/models");
    const local = data && Array.isArray(data.local) ? data.local : [];
    list.textContent = "";
    if (!local.length) list.appendChild(el("li", "muted", "no local models installed"));
    for (const m of local.slice(0, 20)) {
      const li = el("li");
      li.appendChild(el("div", "grow", m.name || m.id || "model"));
      li.appendChild(el("span", "sub", m.sizeBytes ? humanBytes(Number(m.sizeBytes)) : ""));
      const selectBtn = el("button", "btn");
      selectBtn.type = "button";
      selectBtn.textContent = "Select";
      selectBtn.addEventListener("click", async () => {
        await api("/api/models/select", { method: "POST", body: { model: m.name || m.id } });
        notify("ok", "model selection requested: " + (m.name || m.id));
        loadModels();
      });
      li.appendChild(selectBtn);
      list.appendChild(li);
    }
    const badgeEl = $("#chat-session-badge");
    if (data && data.backend) {
      const right = $("#status-right");
      if (right && data.backend) right.textContent = (right.textContent ? right.textContent + " · " : "") + "backend " + data.backend;
    }
  }
  const recs = $("#models-recs");
  if (recs) {
    try {
      const data = await api("/api/models/recommendations");
      const recList = Array.isArray(data) ? data : data && data.recommendations ? data.recommendations : [];
      recs.textContent = "";
      if (!recList.length) recs.appendChild(el("li", "muted", "no recommendations published"));
      for (const r of recList.slice(0, 12)) {
        const li = el("li");
        li.appendChild(el("div", "grow", r.name || r.id || "model"));
        li.appendChild(el("span", "sub", r.reason || r.why || ""));
        recs.appendChild(li);
      }
    } catch (err) {
      recs.textContent = "";
      recs.appendChild(el("li", "muted", "recommendations unavailable"));
    }
  }
}

/* --- tools --------------------------------------------------------------------------- */

loaders.tools = () => {
  bindOnce("#tools-refresh", loadTools);
  every(30000, loadTools);
};

async function loadTools() {
  const list = $("#tools-list");
  if (list) {
    try {
      const data = await api("/api/tools");
      const tools = Array.isArray(data) ? data : data && data.tools ? data.tools : [];
      list.textContent = "";
      if (!tools.length) list.appendChild(el("li", "muted", "no built-in tools published"));
      for (const t of tools.slice(0, 30)) {
        const li = el("li");
        const main = el("div", "grow");
        main.appendChild(el("div", null, t.name || t.id || "tool"));
        main.appendChild(el("div", "sub", t.description || t.desc || ""));
        li.appendChild(main);
        list.appendChild(li);
      }
    } catch (err) {
      list.textContent = "";
      list.appendChild(el("li", "muted", "tool registry unavailable"));
    }
  }
  const custom = $("#custom-tools-list");
  if (custom) {
    try {
      const data = await api("/api/custom-tools");
      const tools = Array.isArray(data) ? data : data && data.tools ? data.tools : [];
      custom.textContent = "";
      if (!tools.length) custom.appendChild(el("li", "muted", "no custom tools installed"));
      for (const t of tools.slice(0, 30)) {
        const li = el("li");
        li.appendChild(el("div", "grow", t.name || t.id || "custom tool"));
        custom.appendChild(li);
      }
    } catch (err) {
      custom.textContent = "";
      custom.appendChild(el("li", "muted", "custom tools unavailable"));
    }
  }
}

/* --- logs ------------------------------------------------------------------------------ */

loaders.logs = () => {
  bindOnce("#logs-refresh", loadLogs);
  every(6000, loadLogs);
};

async function loadLogs() {
  const view = $("#logs-view");
  if (!view) return;
  try {
    const data = await api("/api/logs?limit=200");
    const lines = Array.isArray(data)
      ? data
      : data && Array.isArray(data.lines)
        ? data.lines
        : data && typeof data.text === "string"
          ? data.text.split("\n")
          : [];
    view.textContent = lines.length ? lines.join("\n").slice(-20000) : "the core published no log lines";
  } catch (err) {
    view.textContent = "logs unavailable: " + err.message;
  }
}

/* ------------------------------------------------------------------ */
/* bindOnce: attach an event listener exactly once per element.        */
/* ------------------------------------------------------------------ */

const bound = new WeakSet();

function bindOnce(sel, fn) {
  const node = document.querySelector(sel);
  if (!node || bound.has(node)) return;
  bound.add(node);
  const ev = node.tagName === "FORM" ? "submit" : "click";
  node.addEventListener(ev, (e) => {
    if (ev === "submit") e.preventDefault();
    Promise.resolve(fn(e)).catch((err) => notify("danger", String(err.message || err)));
  });
}

/* ------------------------------------------------------------------ */
/* Router: hash routing, ONE active view, rail state, screen class.    */
/* ------------------------------------------------------------------ */

function resolveClass(width) {
  if (width < 600) return "compact";
  if (width < 900) return "medium";
  if (width < 1280) return "expanded";
  return "wide";
}

function applyClass() {
  const cls = resolveClass(window.innerWidth);
  for (const c of ["compact", "medium", "expanded", "wide"]) {
    document.documentElement.classList.toggle(c, c === cls);
  }
  document.body.classList.toggle("nav-open", document.body.classList.contains("nav-open") && cls === "compact");
}

function currentRoute() {
  const id = window.location.hash.replace(/^#\/?/, "");
  return REGISTRY.some((w) => w.id === id) ? id : "chat";
}

function navigate(id) {
  if (window.location.hash !== "#/" + id) {
    window.location.hash = "#/" + id;
    return;
  }
  show(id);
}

function show(id) {
  clearLoaders();
  for (const w of REGISTRY) {
    const section = document.querySelector('[data-view="' + w.id + '"]');
    if (section) section.hidden = w.id !== id;
    const item = document.getElementById("nav-" + w.id);
    if (item) {
      item.classList.toggle("active", w.id === id);
      if (w.id === id) item.setAttribute("aria-current", "page");
      else item.removeAttribute("aria-current");
    }
  }
  document.body.classList.remove("nav-open");
  const toggle = $("#nav-toggle");
  if (toggle) toggle.setAttribute("aria-expanded", "false");
  const loader = loaders[id];
  if (loader) loader();
  const host = $("#view-host");
  if (host) host.focus({ preventScroll: true });
}

function buildRail() {
  const groups = { primary: $("#rail-primary"), utility: $("#rail-utility") };
  for (const w of REGISTRY) {
    const item = el("button", "rail-item");
    item.type = "button";
    item.id = "nav-" + w.id;
    item.appendChild(icon(w.id));
    const label = el("span", "label", w.title);
    item.appendChild(label);
    if (w.key) {
      const kbd = el("span", "kbd", "Ctrl+" + w.key);
      kbd.className = "kbd";
      item.appendChild(kbd);
    }
    item.addEventListener("click", () => navigate(w.id));
    groups[w.group].appendChild(item);
  }
}

/* Keyboard: Ctrl+1..9 jumps across the primary registry, Ctrl+K palette. */

function setupKeys() {
  document.addEventListener("keydown", (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      openPalette();
      return;
    }
    if ((e.ctrlKey || e.metaKey) && /^[1-9]$/.test(e.key)) {
      const entry = REGISTRY.find((w) => w.key === e.key);
      if (entry) {
        e.preventDefault();
        navigate(entry.id);
      }
    }
  });
}

/* ------------------------------------------------------------------ */
/* Command palette: navigation + the few global commands.              */
/* ------------------------------------------------------------------ */

function paletteCommands(query) {
  const q = query.trim().toLowerCase();
  const nav = REGISTRY.map((w) => ({
    label: "Go to " + w.title,
    run: () => navigate(w.id)
  }));
  const cmds = [
    { label: "Refresh core status", run: () => refreshHealth() },
    { label: "Refresh engine state", run: () => loadEngines() },
    { label: "Run network check", run: () => {
      navigate("system");
      const btn = $("#netcheck-btn");
      if (btn) btn.click();
    } },
    { label: "Check for engine update", run: () => {
      navigate("engines");
      const btn = $("#engine-update");
      if (btn) btn.click();
    } }
  ];
  const all = nav.concat(cmds);
  if (!q) return all;
  return all.filter((c) => c.label.toLowerCase().includes(q));
}

let paletteIndex = 0;

function renderPalette() {
  const input = $("#palette-input");
  const list = $("#palette-list");
  if (!input || !list) return;
  const items = paletteCommands(input.value);
  list.textContent = "";
  paletteIndex = Math.min(paletteIndex, Math.max(0, items.length - 1));
  items.forEach((c, i) => {
    const li = el("li", i === paletteIndex ? "selected" : "");
    li.textContent = c.label;
    li.addEventListener("click", () => {
      closePalette();
      c.run();
    });
    list.appendChild(li);
  });
  return items;
}

function openPalette() {
  const backdrop = $("#palette-backdrop");
  if (!backdrop) return;
  backdrop.hidden = false;
  const input = $("#palette-input");
  if (input) {
    input.value = "";
    input.focus();
  }
  paletteIndex = 0;
  renderPalette();
}

function closePalette() {
  const backdrop = $("#palette-backdrop");
  if (backdrop) backdrop.hidden = true;
}

function setupPalette() {
  const openBtn = $("#palette-open");
  if (openBtn) openBtn.appendChild(icon("tools"));
  const backdrop = $("#palette-backdrop");
  const input = $("#palette-input");
  if (openBtn) openBtn.addEventListener("click", openPalette);
  if (input) input.addEventListener("input", () => {
    paletteIndex = 0;
    renderPalette();
  });
  if (backdrop) {
    backdrop.addEventListener("click", (e) => {
      if (e.target === backdrop) closePalette();
    });
  }
  document.addEventListener("keydown", (e) => {
    if (backdrop.hidden) return;
    if (e.key === "Escape") {
      closePalette();
      return;
    }
    const items = paletteCommands(input.value);
    if (e.key === "ArrowDown") {
      e.preventDefault();
      paletteIndex = Math.min(paletteIndex + 1, items.length - 1);
      renderPalette();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      paletteIndex = Math.max(paletteIndex - 1, 0);
      renderPalette();
    } else if (e.key === "Enter" && items[paletteIndex]) {
      e.preventDefault();
      closePalette();
      items[paletteIndex].run();
    }
  });
}

/* ------------------------------------------------------------------ */
/* Boot.                                                               */
/* ------------------------------------------------------------------ */

function boot() {
  const mark = $("#brand-mark");
  if (mark) mark.appendChild(icon("system"));
  const paletteBtn = $("#palette-open");
  if (paletteBtn && !paletteBtn.childElementCount) paletteBtn.appendChild(icon("tools"));
  const navToggle = $("#nav-toggle");
  if (navToggle) navToggle.appendChild(icon("logs"));
  buildRail();
  setupComposer();
  setupPalette();
  setupKeys();
  window.addEventListener("hashchange", () => show(currentRoute()));
  window.addEventListener("resize", applyClass);
  applyClass();
  show(currentRoute());
  startHealth();
  startPolling();
}

document.addEventListener("DOMContentLoaded", boot);
