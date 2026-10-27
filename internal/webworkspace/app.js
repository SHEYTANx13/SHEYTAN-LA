// app.js — the Web Workspace renderer (v1.11.0).
//
// Hard rules, enforced by construction in this file:
//   - NO HTML-injection DOM sinks ANYWHERE: every text node is built
//     with createTextNode and every element with createElement; no
//     property that parses a string into markup is used, named, or
//     aliased. A hostile payload cannot inject markup even in
//     principle (the CSP also forbids inline script, but the DOM
//     discipline does not rely on the CSP).
//   - Links are created ONLY for http/https hrefs (javascript:, data:,
//     and every other scheme renders as plain text), always
//     rel="noopener noreferrer" and target="_blank".
//   - The only fetch is to this page's own origin (CSP connect-src
//     'self'); the bearer token stays in memory, never in a URL the
//     page sends, never logged, never written to storage.
//   - Unknown block kinds render as raw data (never blank, never a
//     crash), with the original kind surfaced in the meta row.
//   - Degrades honestly: token failure, fetch failure and empty state
//     each get a distinct, actionable message. The native window remains
//     the authority; this page renders only.

const TOKEN = (() => {
  // The launch URL carries the token in the fragment (#t=…) which the
  // browser never sends to the server. Read once, then scrub the
  // address bar so the token cannot linger in visible UI or history.
  const m = /^#t=([0-9a-f]{64})$/.exec(location.hash || "");
  try { history.replaceState(null, "", location.pathname); } catch (_) { /* ignore */ }
  return m ? m[1] : "";
})();

const els = {
  content: document.getElementById("content"),
  conn: document.getElementById("conn"),
  refresh: document.getElementById("refresh"),
};

function setConn(text, cls) {
  els.conn.textContent = text;
  els.conn.className = "conn" + (cls ? " " + cls : "");
}

function el(tag, cls) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  return e;
}

function textNode(t) { return document.createTextNode(t); }

// ---------------------------------------------------------------------------
// Markdown safe subset → DOM. Headings, paragraphs, lists, blockquotes,
// fenced code, inline code, bold/italic, and http/https-only links.
// Everything else passes through as literal text. Math and citation
// syntax are NOT rendered — they degrade to text (honest degradation;
// those capabilities are planned, not shipped).
// ---------------------------------------------------------------------------

const INLINE = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*]+\*)|(\[[^\]]+\]\(https?:\/\/[^)\s]+\))/g;

function inlineInto(target, line) {
  let last = 0, m;
  INLINE.lastIndex = 0;
  while ((m = INLINE.exec(line)) !== null) {
    if (m.index > last) target.appendChild(textNode(line.slice(last, m.index)));
    const tok = m[0];
    if (tok.startsWith("`")) {
      const c = el("code");
      c.appendChild(textNode(tok.slice(1, -1)));
      target.appendChild(c);
    } else if (tok.startsWith("**")) {
      const b = el("strong");
      b.appendChild(textNode(tok.slice(2, -2)));
      target.appendChild(b);
    } else if (tok.startsWith("*")) {
      const i = el("em");
      i.appendChild(textNode(tok.slice(1, -1)));
      target.appendChild(i);
    } else {
      // [label](http…) — scheme allow-list is part of the regex; the
      // href is still re-validated below before an anchor is created.
      const cut = tok.indexOf("](");
      const label = tok.slice(1, cut);
      const href = tok.slice(cut + 2, -1);
      const a = safeLink(href, label);
      if (a) target.appendChild(a);
      else target.appendChild(textNode(label + " (" + href + ")"));
    }
    last = m.index + tok.length;
  }
  if (last < line.length) target.appendChild(textNode(line.slice(last)));
}

function safeLink(href, label) {
  try {
    const u = new URL(href, location.href);
    if (u.protocol !== "http:" && u.protocol !== "https:") return null;
    const a = el("a");
    a.href = u.href;
    a.rel = "noopener noreferrer";
    a.target = "_blank";
    a.appendChild(textNode(label || u.href));
    return a;
  } catch (_) {
    return null;
  }
}

function renderMarkdown(container, payload) {
  const box = el("div", "md");
  const lines = payload.split("\n");
  let i = 0;
  let para = null;
  const flushPara = () => { if (para && para.hasChildNodes()) box.appendChild(para); para = null; };
  while (i < lines.length) {
    const line = lines[i];
    const fence = /^```(\w*)\s*$/.exec(line.trim());
    if (fence) {
      flushPara();
      const body = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i].trim())) { body.push(lines[i]); i++; }
      i++; // closing fence
      const pre = el("pre");
      pre.appendChild(textNode(body.join("\n")));
      box.appendChild(pre);
      continue;
    }
    const h = /^(#{1,3})\s+(.*)$/.exec(line);
    if (h) {
      flushPara();
      const tag = "h" + h[1].length;
      const node = el(tag);
      inlineInto(node, h[2]);
      box.appendChild(node);
      i++;
      continue;
    }
    const quote = /^>\s?(.*)$/.exec(line);
    if (quote) {
      flushPara();
      const q = el("blockquote");
      inlineInto(q, quote[1]);
      box.appendChild(q);
      i++;
      continue;
    }
    const ul = /^\s*[-*]\s+(.*)$/.exec(line);
    const ol = /^\s*\d+[.)]\s+(.*)$/.exec(line);
    if (ul || ol) {
      flushPara();
      const list = el(ul ? "ul" : "ol");
      const re = ul ? /^\s*[-*]\s+(.*)$/ : /^\s*\d+[.)]\s+(.*)$/;
      while (i < lines.length) {
        const mm = re.exec(lines[i]);
        if (!mm) break;
        const li = el("li");
        inlineInto(li, mm[1]);
        list.appendChild(li);
        i++;
      }
      box.appendChild(list);
      continue;
    }
    if (line.trim() === "") { flushPara(); i++; continue; }
    if (!para) para = el("p");
    if (para.hasChildNodes()) para.appendChild(textNode(" "));
    inlineInto(para, line.trim());
    i++;
  }
  flushPara();
  container.appendChild(box);
}

function renderCode(container, block) {
  const pre = el("pre");
  pre.dataset.lang = block.language || "";
  pre.appendChild(textNode(block.payload));
  container.appendChild(pre);
}

function renderDiff(container, payload) {
  const pre = el("pre");
  for (const line of payload.split("\n")) {
    const span = el("span");
    if (line.startsWith("+") && !line.startsWith("+++")) span.className = "diff-add";
    else if (line.startsWith("-") && !line.startsWith("---")) span.className = "diff-del";
    else if (line.startsWith("@@")) span.className = "diff-hunk";
    span.appendChild(textNode(line === "" ? " " : line));
    pre.appendChild(span);
  }
  container.appendChild(pre);
}

function renderTable(container, block) {
  const rows = block.payload.split("\n").map(r => r.trim()).filter(r => r !== "");
  if (rows.length === 0) {
    const p = el("p", "placeholder");
    p.appendChild(textNode("Empty table."));
    container.appendChild(p);
    return;
  }
  const sep = rows[0].split(",").length;
  const table = el("table");
  const head = el("tr");
  for (const cell of rows[0].split(",")) {
    const th = el("th");
    th.scope = "col";
    th.appendChild(textNode(cell.trim()));
    head.appendChild(th);
  }
  table.appendChild(head);
  for (let i = 1; i < rows.length; i++) {
    const tr = el("tr");
    const cells = rows[i].split(",");
    for (let c = 0; c < sep; c++) {
      const td = el("td");
      td.appendChild(textNode(c < cells.length ? cells[c].trim() : ""));
      tr.appendChild(td);
    }
    table.appendChild(tr);
  }
  container.appendChild(table);
}

// renderChartSpec draws a bounded bar/line chart as inline SVG built
// with createElementNS (no markup parsing anywhere). Anything that does
// not fit the deterministic spec degrades to a data table.
function renderChartSpec(container, block) {
  let spec = null;
  try { spec = JSON.parse(block.payload); } catch (_) { spec = null; }
  const usable = spec && typeof spec === "object" &&
    (spec.kind === "bar" || spec.kind === "line") &&
    Array.isArray(spec.labels) && Array.isArray(spec.values) &&
    spec.labels.length === spec.values.length &&
    spec.labels.length > 0 && spec.labels.length <= 64 &&
    spec.values.every(v => typeof v === "number" && isFinite(v));
  if (!usable) {
    const note = el("p", "chart-note");
    note.appendChild(textNode("Chart spec not renderable — showing the raw data below."));
    container.appendChild(note);
    const pre = el("pre");
    pre.appendChild(textNode(block.payload));
    container.appendChild(pre);
    return;
  }
  const n = spec.values.length;
  const W = Math.max(320, n * 46), H = 220, pad = 34;
  const maxV = Math.max(...spec.values, 0), minV = Math.min(...spec.values, 0);
  const range = (maxV - minV) || 1;
  const y = v => H - pad - ((v - minV) / range) * (H - pad * 2);
  const NS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", "0 0 " + W + " " + H);
  svg.setAttribute("role", "img");
  const title = document.createElementNS(NS, "title");
  title.appendChild(textNode(block.title || "chart"));
  svg.appendChild(title);
  // Zero line.
  const zero = document.createElementNS(NS, "line");
  zero.setAttribute("x1", pad); zero.setAttribute("x2", W - pad / 2);
  zero.setAttribute("y1", y(0)); zero.setAttribute("y2", y(0));
  zero.setAttribute("stroke", "currentColor"); zero.setAttribute("stroke-width", "0.75");
  svg.appendChild(zero);
  const slot = (W - pad - 8) / n;
  if (spec.kind === "bar") {
    for (let i = 0; i < n; i++) {
      const rect = document.createElementNS(NS, "rect");
      const top = Math.max(y(spec.values[i]), y(0));
      rect.setAttribute("x", (pad + i * slot + slot * 0.15).toFixed(2));
      rect.setAttribute("y", top.toFixed(2));
      rect.setAttribute("width", (slot * 0.7).toFixed(2));
      rect.setAttribute("height", Math.max(1, Math.abs(y(spec.values[i]) - y(0))).toFixed(2));
      rect.setAttribute("fill", "#4f46e5");
      svg.appendChild(rect);
    }
  } else {
    const poly = document.createElementNS(NS, "polyline");
    const pts = spec.values.map((v, i) =>
      (pad + i * slot + slot / 2).toFixed(2) + "," + y(v).toFixed(2)).join(" ");
    poly.setAttribute("points", pts);
    poly.setAttribute("fill", "none");
    poly.setAttribute("stroke", "#4f46e5");
    poly.setAttribute("stroke-width", "2");
    svg.appendChild(poly);
  }
  // Labels (first/last only when dense — no overlap by construction).
  const step = n > 12 ? Math.ceil(n / 8) : 1;
  for (let i = 0; i < n; i += step) {
    const t = document.createElementNS(NS, "text");
    t.setAttribute("x", (pad + i * slot + slot / 2).toFixed(2));
    t.setAttribute("y", (H - pad + 14).toFixed(2));
    t.setAttribute("text-anchor", "middle");
    t.setAttribute("font-size", "10");
    t.setAttribute("fill", "currentColor");
    t.appendChild(textNode(String(spec.labels[i]).slice(0, 12)));
    svg.appendChild(t);
  }
  const wrap = el("div", "chart-wrap");
  wrap.appendChild(svg);
  container.appendChild(wrap);
  const note = el("p", "chart-note");
  note.appendChild(textNode(spec.kind + " · " + n + " points · rendered from the deterministic spec (not model HTML)"));
  container.appendChild(note);
}

function renderRawData(container, block) {
  const pre = el("pre", "rawdata");
  pre.appendChild(textNode(block.payload === "" ? "(empty payload)" : block.payload));
  container.appendChild(pre);
}

// renderBlock routes on the envelope kind. Unknown kinds land in
// renderRawData with the original kind surfaced — the safe generic
// raw-data state, never a blank page.
function renderBlock(block) {
  const card = el("article", "block");
  const meta = el("div", "meta");
  const kind = el("span", "badge");
  kind.appendChild(textNode(block.kind + (block.language ? " · " + block.language : "")));
  meta.appendChild(kind);
  if (block.id) {
    const id = el("span");
    id.appendChild(textNode("id " + block.id));
    meta.appendChild(id);
  }
  if (block.runId) {
    const run = el("span", "badge run");
    run.appendChild(textNode("run " + block.runId + (block.sessionId ? " · session " + block.sessionId : "")));
    meta.appendChild(run);
  }
  if (block.source) {
    const src = el("span");
    src.appendChild(textNode("source: " + block.source));
    meta.appendChild(src);
  }
  card.appendChild(meta);
  if (block.title) {
    const h = el("h2");
    h.style.fontSize = "15px";
    h.appendChild(textNode(block.title));
    card.appendChild(h);
  }
  // Envelope gate: a future envelope version degrades to raw data with
  // a note — the renderer never guesses.
  if (block.version !== 1) {
    const note = el("p", "chart-note");
    note.appendChild(textNode("Envelope version " + block.version + " not understood by this renderer — raw data below."));
    card.appendChild(note);
    renderRawData(card, block);
    return card;
  }
  switch (block.kind) {
    case "markdown": renderMarkdown(card, block); break;
    case "code": renderCode(card, block); break;
    case "diff": renderDiff(card, block.payload); break;
    case "text":
    case "status": {
      const p = el("p");
      p.appendChild(textNode(block.payload));
      card.appendChild(p);
      break;
    }
    case "table": renderTable(card, block); break;
    case "chartspec": renderChartSpec(card, block); break;
    case "artifact":
    case "scene3d":
    default: renderRawData(card, block); break;
  }
  return card;
}

async function load() {
  if (!TOKEN) {
    els.content.replaceChildren();
    const box = el("div", "error-box");
    box.appendChild(textNode("This page was opened without the launch token " +
      "(the address lost the #t= fragment). Close it and open the Workspace " +
      "again from the application — the token is issued per launch and is " +
      "never displayed here."));
    els.content.appendChild(box);
    setConn("no token", "err");
    return;
  }
  setConn("loading…", "");
  let resp;
  try {
    resp = await fetch("/api/blocks", { headers: { "Authorization": "Bearer " + TOKEN } });
  } catch (e) {
    failState("The workspace endpoint could not be reached (" + e.message + "). " +
      "The host may have been stopped from the application. Content in the " +
      "native window is unaffected.");
    return;
  }
  if (resp.status === 401) {
    failState("The launch token was rejected (the workspace was restarted " +
      "or the link is older than the current launch). Reopen the Workspace " +
      "from the application for a fresh link.");
    return;
  }
  if (!resp.ok) {
    failState("The workspace endpoint answered " + resp.status + " " + resp.statusText + ".");
    return;
  }
  let data;
  try { data = await resp.json(); }
  catch (_) { failState("The workspace endpoint returned malformed data."); return; }
  if (!data || typeof data !== "object" || typeof data.version !== "number") {
    failState("The workspace endpoint returned an envelope this renderer cannot interpret.");
    return;
  }
  const blocks = Array.isArray(data.blocks) ? data.blocks : [];
  els.content.replaceChildren();
  if (blocks.length === 0) {
    const empty = el("div", "empty");
    empty.appendChild(textNode("No content blocks have been published yet. " +
      "Finish or stream a run in the application and use “Open in Workspace”, " +
      "or publish from a tool — this page always renders what the core " +
      "authorities published, never its own copy."));
    els.content.appendChild(empty);
    setConn("empty", "");
    return;
  }
  const frag = document.createDocumentFragment();
  for (const b of blocks) frag.appendChild(renderBlock(b));
  els.content.appendChild(frag);
  setConn(blocks.length + " block" + (blocks.length === 1 ? "" : "s") + " · envelope v" + data.version, "ok");
}

function failState(msg) {
  els.content.replaceChildren();
  const box = el("div", "error-box");
  box.appendChild(textNode(msg));
  const btn = el("button");
  btn.type = "button";
  btn.appendChild(textNode("Retry"));
  btn.addEventListener("click", load);
  box.appendChild(btn);
  els.content.appendChild(box);
  setConn("error", "err");
}

els.refresh.addEventListener("click", load);
load();
