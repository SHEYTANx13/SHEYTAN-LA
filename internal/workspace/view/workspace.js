// SHEYTAN-LA Web Workspace — offline bundled viewer.
//
// Security contract (enforced by review + tests on the Go side):
//   - Every text node is created via textContent. The markup-injection
//     string-assignment APIs (the HTML/adjacent-HTML setters and
//     document writing) are never used anywhere in this file, so
//     model-generated payload bytes can never inject markup.
//   - The launch token lives in the URL fragment (never sent to the
//     server); it is read once and used ONLY in the Authorization header.
//   - Links: only http:, https: and mailto: become anchors; every other
//     scheme is downgraded to plain text. rel="noopener noreferrer".
//   - No eval, no remote imports, no telemetry: the CSP would reject them
//     and the code below never attempts them.

const TOKEN = (() => {
  const m = /^\s*#?\s*t=([0-9a-f]+)\s*$/.exec(location.hash || "");
  if (m) {
    // Remove the fragment from the visible URL: the token must not linger
    // in the address bar, the history stack, or a copied URL.
    history.replaceState(null, "", location.pathname);
    return m[1];
  }
  return "";
})();

const conn = document.getElementById("conn");
const listEl = document.getElementById("blocks");
const emptyEl = document.getElementById("empty");
const detailEl = document.getElementById("detail");
let selected = null;

function setStatus(text, isError) {
  conn.textContent = text;
  conn.classList.toggle("error", Boolean(isError));
}

function authHeaders() {
  return TOKEN ? { "Authorization": "Bearer " + TOKEN } : {};
}

async function fetchJSON(url) {
  const res = await fetch(url, { headers: authHeaders(), cache: "no-store" });
  if (res.status === 401) throw new Error("missing or invalid launch token");
  if (res.status === 404) throw new Error("not found");
  if (!res.ok) throw new Error("server responded " + res.status);
  return res.json();
}

function el(tag, cls) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  return n;
}

function text(tag, value, cls) {
  const n = el(tag, cls);
  n.textContent = value; // the ONLY text path: no markup interpretation
  return n;
}

// safeLink: an anchor for http/https/mailto URLs, plain text otherwise.
function safeLink(href, label) {
  let u = null;
  try { u = new URL(href, location.href); } catch { u = null; }
  const scheme = u ? u.protocol.replace(/:$/, "") : "";
  if (u && u.origin === location.origin) {
    const a = document.createElement("a");
    a.href = u.pathname + u.search; // same-origin path only
    a.textContent = label;
    return a;
  }
  if (scheme === "http" || scheme === "https" || scheme === "mailto") {
    const a = document.createElement("a");
    a.href = href;
    a.textContent = label;
    a.rel = "noopener noreferrer";
    a.target = "_blank";
    return a;
  }
  return text("span", label);
}

// ---------- kind renderers (all DOM-safe) ----------

function renderInline(target, value) {
  // Minimal inline markdown: **bold**, *italic*, `code`. Everything else
  // stays literal text. The scanner never interprets HTML entities.
  const re = /\*\*([^*]+)\*\*|\*([^*]+)\*|`([^`]+)`/g;
  let last = 0, m;
  while ((m = re.exec(value)) !== null) {
    if (m.index > last) target.appendChild(text("span", value.slice(last, m.index)));
    if (m[1] !== undefined) target.appendChild(text("strong", m[1]));
    else if (m[2] !== undefined) target.appendChild(text("em", m[2]));
    else if (m[3] !== undefined) target.appendChild(text("code", m[3]));
    last = re.lastIndex;
  }
  if (last < value.length) target.appendChild(text("span", value.slice(last)));
}

function renderMarkdown(target, raw) {
  const box = el("div", "md");
  const lines = String(raw).split(/\r?\n/);
  let para = null, list = null, fence = null, fenceLang = "";
  const closePara = () => { para = null; };
  const closeList = () => { list = null; };

  for (const rawLine of lines) {
    if (fence !== null) {
      if (/^```/.test(rawLine)) {
        target.appendChild(fence); fence = null; continue;
      }
      fence.firstChild.appendChild(text("code", rawLine + "\n"));
      continue;
    }
    const line = rawLine;
    const fenceMatch = /^```\s*(\S*)\s*$/.exec(line);
    if (fenceMatch) {
      closePara(); closeList();
      fence = el("pre");
      const code = el("code");
      fence.appendChild(code);
      if (fenceMatch[1]) {
        const tag = text("span", fenceMatch[1]);
        tag.className = "lang";
        fence.appendChild(tag);
      }
      continue;
    }
    const h = /^(#{1,3})\s+(.*)$/.exec(line);
    if (h) {
      closePara(); closeList();
      const level = ["h1", "h2", "h3"][h[1].length - 1];
      const head = el(level);
      renderInline(head, h[2]);
      target.appendChild(head);
      continue;
    }
    const li = /^\s*[-*]\s+(.*)$/.exec(line);
    if (li) {
      closePara();
      if (!list || list.tagName !== "UL") { list = el("ul"); target.appendChild(list); }
      const item = el("li");
      renderInline(item, li[1]);
      list.appendChild(item);
      continue;
    }
    const oli = /^\s*\d+[.)]\s+(.*)$/.exec(line);
    if (oli) {
      closePara();
      if (!list || list.tagName !== "OL") { list = el("ol"); target.appendChild(list); }
      const item = el("li");
      renderInline(item, oli[1]);
      list.appendChild(item);
      continue;
    }
    if (line.trim() === "") { closePara(); closeList(); continue; }
    closeList();
    if (!para) { para = el("p"); target.appendChild(para); }
    else para.appendChild(text("span", " "));
    renderInline(para, line.trim());
  }
  if (fence !== null) target.appendChild(fence); // unterminated fence: keep the text
  target.appendChild(box);
}

function renderCode(target, raw, meta) {
  const box = el("div", "codeblock");
  const lang = (meta && meta.lang) || "code";
  box.appendChild(text("div", String(lang), "lang"));
  const pre = el("pre");
  pre.appendChild(text("code", String(raw)));
  box.appendChild(pre);
  target.appendChild(box);
}

function renderDiff(target, raw) {
  const box = el("div", "diff codeblock");
  const pre = el("pre");
  for (const line of String(raw).split(/\r?\n/)) {
    let cls = "ln-ctx", body = line;
    if (line.startsWith("+") && !line.startsWith("+++")) { cls = "ln-add"; }
    else if (line.startsWith("-") && !line.startsWith("---")) { cls = "ln-del"; }
    else if (line.startsWith("@@")) { body = line; }
    pre.appendChild(text("span", body, cls));
  }
  box.appendChild(pre);
  target.appendChild(box);
}

function isNumericCell(v) {
  return typeof v === "number" || (typeof v === "string" && v !== "" && !isNaN(Number(v)));
}

function renderTable(target, payload) {
  let t;
  try { t = JSON.parse(String(payload)); } catch { return renderRaw(target, payload, "table payload was not valid JSON"); }
  const cols = Array.isArray(t.columns) ? t.columns.map(String) : [];
  const rows = Array.isArray(t.rows) ? t.rows : [];
  if (!cols.length && !rows.length) return renderRaw(target, payload, "table payload had no columns or rows");
  const table = el("table", "data");
  if (cols.length) {
    const thead = el("thead");
    const tr = el("tr");
    for (const c of cols) tr.appendChild(text("th", c));
    thead.appendChild(tr);
    table.appendChild(thead);
  }
  const tbody = el("tbody");
  let shown = 0;
  const MAX_ROWS = 500;
  for (const row of rows) {
    if (shown >= MAX_ROWS) break;
    const cells = Array.isArray(row) ? row : [row];
    const tr = el("tr");
    for (const c of cells) {
      const v = (c === null || c === undefined) ? "" : String(c);
      tr.appendChild(text("td", v, isNumericCell(c) ? "num" : undefined));
    }
    tbody.appendChild(tr);
    shown++;
  }
  table.appendChild(tbody);
  target.appendChild(table);
  if (rows.length > MAX_ROWS) {
    target.appendChild(text("p", `Showing ${MAX_ROWS} of ${rows.length} rows — the full table lives in the native application.`, "hint"));
  }
}

function renderChart(target, payload, block) {
  let obj;
  try { obj = JSON.parse(String(payload)); } catch { return renderRaw(target, payload, "chart payload was not valid JSON"); }
  const box = el("div", "chartspec");
  const title = obj && typeof obj.type === "string" ? `chart-spec — type: ${obj.type}` : "chart-spec";
  box.appendChild(text("div", title, "lang"));
  box.appendChild(text("pre", JSON.stringify(obj, null, 2)));
  target.appendChild(box);
  target.appendChild(text("p",
    "Structured chart specification (safe JSON view). Pixel-perfect chart rendering and WebGL content stay on the native surfaces and the 3D Studio; this viewer never executes chart code.",
    "hint"));
}

function renderRaw(target, payload, note, block) {
  if (note) target.appendChild(text("p", note, "degrade-note"));
  if (block && block.degrade) {
    target.appendChild(text("p", `Degraded (${block.degrade.reason}): ${block.degrade.detail || "this block is rendered as safe raw data"}`, "degrade-note"));
  }
  const pre = el("div", "rawdata");
  pre.textContent = typeof payload === "string" ? payload : JSON.stringify(payload, null, 2);
  target.appendChild(pre);
}

function renderArtifact(target, block) {
  const card = el("div");
  card.appendChild(text("p", `Artifact reference: ${block.path || "(no path recorded)"}`));
  card.appendChild(text("p", "Artifact content lives in the Go artifact authority; open it from the native application (Work Environment → files).", "hint"));
  target.appendChild(card);
}

function render3D(target) {
  const card = el("div");
  card.appendChild(text("p", "3D content is owned by the read-only 3D Studio."));
  card.appendChild(text("p", "Open it from the navigation rail in the native application — this viewer does not embed or proxy the Studio.", "hint"));
  target.appendChild(card);
}

// ---------- detail rendering ----------

function renderDetail(block) {
  detailEl.textContent = "";
  const card = el("div", "card");
  card.appendChild(text("h2", block.title || block.id));
  const meta = [
    `kind: ${block.kind}`,
    `id: ${block.id}`,
    block.runId ? `run: ${block.runId}` : null,
    block.sessionId ? `session: ${block.sessionId}` : null,
    block.source ? `source: ${block.source}` : null,
  ].filter(Boolean).join("  ·  ");
  card.appendChild(text("div", meta, "meta"));

  const payload = block.payload;
  const asText = typeof payload === "string" ? payload : (payload === undefined || payload === null ? "" : String(payload));

  switch (block.kind) {
    case "markdown": renderMarkdown(card, asText); break;
    case "code": renderCode(card, asText, { lang: (block.capabilities || [])[0] }); break;
    case "diff": renderDiff(card, asText); break;
    case "table": renderTable(card, asText); break;
    case "chart-spec": renderChart(card, asText, block); break;
    case "text":
    case "status": {
      const pre = el("pre", "rawdata");
      pre.textContent = asText;
      card.appendChild(pre);
      break;
    }
    case "artifact": renderArtifact(card, block); break;
    case "3d": render3D(card); break;
    default: renderRaw(card, asText, null, block); break; // unknown kinds: safe raw data
  }
  detailEl.appendChild(card);
}

// ---------- list ----------

async function loadList() {
  setStatus("loading…", false);
  try {
    const data = await fetchJSON("/api/workspace/blocks");
    const blocks = Array.isArray(data.blocks) ? data.blocks : [];
    listEl.textContent = "";
    emptyEl.hidden = blocks.length > 0;
    if (!blocks.length) {
      setStatus("no blocks", false);
      return;
    }
    for (const m of blocks) {
      const li = el("li");
      li.tabIndex = 0;
      li.setAttribute("role", "option");
      li.dataset.id = m.id;
      li.appendChild(text("span", String(m.kind), "k"));
      li.appendChild(text("span", m.title || m.id, "t"));
      if (m.degrade) li.appendChild(text("span", " · degraded", "k"));
      li.addEventListener("click", () => select(m.id));
      li.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); select(m.id); } });
      listEl.appendChild(li);
    }
    setStatus(`${blocks.length} block${blocks.length === 1 ? "" : "s"}`, false);
    if (selected) {
      const still = blocks.find((b) => b.id === selected);
      if (!still) { selected = null; showPlaceholder(); }
    }
  } catch (err) {
    setStatus("workspace source unavailable: " + err.message, true);
    // The native application continues to render everything; this surface
    // failing must never take content down with it.
  }
}

function showPlaceholder() {
  detailEl.textContent = "";
  const p = el("div", "empty");
  p.appendChild(text("p", "Select a block on the left to render it here."));
  detailEl.appendChild(p);
}

async function select(id) {
  selected = id;
  for (const li of listEl.children) {
    li.setAttribute("aria-selected", li.dataset.id === id ? "true" : "false");
  }
  setStatus("loading block…", false);
  try {
    const block = await fetchJSON("/api/workspace/block?id=" + encodeURIComponent(id));
    renderDetail(block);
    setStatus("ready", false);
  } catch (err) {
    detailEl.textContent = "";
    const card = el("div", "card");
    card.appendChild(text("p", "Could not load block: " + err.message, "error"));
    card.appendChild(text("p", "The original content remains intact and rendered in the native application.", "hint"));
    detailEl.appendChild(card);
    setStatus("block load failed", true);
  }
}

document.getElementById("refresh").addEventListener("click", loadList);
document.addEventListener("keydown", (e) => {
  if ((e.key === "r" || e.key === "R") && !e.ctrlKey && !e.metaKey && !e.altKey) loadList();
});

loadList();
