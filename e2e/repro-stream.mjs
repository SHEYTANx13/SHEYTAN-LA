// repro-stream.mjs — REPRODUCTION HARNESS for the v1.8.2 P0 visibility bug.
//
// Drives the REAL stack (server + embedded frontend + native engine) with
// a real Chromium, traces every WebSocket frame the browser receives with
// timestamps, and samples the live generation surface's DOM while the run
// is still active. Answers, with evidence:
//
//   1. does the backend stream `response`/`reasoning` frames to the browser?
//   2. does the Zustand-backed live surface (.generation-bubble) paint them
//      while the run is live?
//   3. what are the measured stage times: request accepted → first response
//      frame → first visible DOM text → completion?
//
// Pure diagnostics — asserts nothing; prints a measured timeline.

import { chromium, expect } from "@playwright/test";
import { startSheytan } from "./fixtures/sheytan-server.ts";

const stack = await startSheytan();
const log = (...a) => console.log(...a);

// The WebSocket frame trace lives in the page world; the init script
// installs it BEFORE the app boots so not a single early frame is lost.
const initScript = `
  window.__wsFrames = [];
  window.__wsOpens = [];
  const OrigWS = window.WebSocket;
  window.WebSocket = function (url) {
    const ws = new OrigWS(url);
    window.__wsOpens.push({ url, t: performance.now() });
    ws.addEventListener("message", (ev) => {
      try {
        const p = JSON.parse(ev.data);
        window.__wsFrames.push({
          t: performance.now(),
          type: p.type,
          seq: p.seq,
          runId: p.runId,
          len: typeof p.caption === "string" ? p.caption.length : undefined,
          phase: p.phase,
          running: p.running,
        });
      } catch {}
    });
    return ws;
  };
  window.WebSocket.prototype = OrigWS.prototype;
  Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
`;

const browser = await chromium.launch();
const page = await browser.newPage();

await page.addInitScript(initScript);

// Console + page errors — never swallow a frontend crash.
page.on("console", (m) => {
  if (m.type() === "error" || m.type() === "warning") {
    log(`[console:${m.type()}]`, m.text().slice(0, 300));
  }
});
page.on("pageerror", (e) => log("[pageerror]", String(e).slice(0, 300)));

await page.goto(stack.baseURL + "/", { waitUntil: "domcontentloaded" });

const composer = page.getByPlaceholder("Message SHEYTAN...");
await composer.waitFor({ state: "visible", timeout: 30_000 });
await expect(composer).toBeEnabled({ timeout: 60_000 });

const tSend = Date.now();
await composer.fill("hi");
await page.getByRole("button", { name: "Send", exact: true }).click();

// Poll the LIVE generation surface every 100 ms while the run is live.
// We record: phase label, bubble text length, whether the run is still
// live (Stop button visible). The instant the phase leaves the live set
// the polling stops.
const domSamples = [];

for (let i = 0; i < 1200; i++) {
  const sample = await page.evaluate(() => {
    const bubble = document.querySelector(".generation-bubble");
    const phase = document.querySelector(".generation-phase");
    const content = bubble?.querySelector(".message-content");
    const stopBtn = Array.from(document.querySelectorAll("button")).some(
      (b) => b.textContent?.trim() === "Stop",
    );
    const reasoning = document.querySelector(".message-reasoning-body");
    return {
      t: performance.now(),
      hasBubble: Boolean(bubble),
      phase: phase?.textContent?.trim() ?? null,
      textLen: content?.textContent?.replace(/\u200b/g, "").length ?? 0,
      textHead: (content?.textContent ?? "").slice(0, 60),
      reasoningLen: reasoning?.textContent?.length ?? 0,
      stopVisible: stopBtn,
    };
  });

  domSamples.push(sample);

  if (!sample.stopVisible && i > 2) {
    // Stop gone → the run settled (or was never live). One final sample
    // captured above; stop polling.
    break;
  }

  await page.waitForTimeout(100);
}

const tAfter = Date.now();
const frames = await page.evaluate(() => window.__wsFrames);
const wsOpens = await page.evaluate(() => window.__wsOpens);

// ---- measured timeline ----------------------------------------------------
log("\n=== WS OPENS ===");
for (const o of wsOpens) log(`  t=${o.t.toFixed(0)}ms ${o.url}`);

log("\n=== WS FRAMES (browser-received) ===");
const t0 = frames[0]?.t ?? 0;
for (const f of frames) {
  const fields = [`t=${f.t.toFixed(0)}ms`, `+${(f.t - t0).toFixed(0)}ms`, f.type];
  if (f.seq !== undefined) fields.push(`seq=${f.seq}`);
  if (f.len !== undefined) fields.push(`len=${f.len}`);
  if (f.phase) fields.push(`phase=${f.phase}`);
  if (f.running !== undefined) fields.push(`running=${f.running}`);
  log("  ", fields.join(" "));
}

log("\n=== DOM SAMPLES (live-surface poll, 100ms cadence) ===");
for (const s of domSamples) {
  log(
    `  t=${s.t.toFixed(0)}ms bubble=${s.hasBubble} phase=${s.phase} ` +
      `textLen=${s.textLen} reasonLen=${s.reasoningLen} stop=${s.stopVisible}` +
      (s.textLen > 0 ? ` head="${s.textHead.replace(/\n/g, " ")}"` : ""),
  );
}

// Summary of visibility evidence.
const responseFrames = frames.filter((f) => f.type === "response");
const firstResponseFrame = responseFrames[0];
const firstVisible = domSamples.find((s) => s.textLen > 0);
const lastLive = [...domSamples].reverse().find((s) => s.stopVisible);

log("\n=== SUMMARY ===");
log(`  wall: send→poll-end: ${tAfter - tSend}ms`);
log(`  response frames received by browser: ${responseFrames.length}`);
if (firstResponseFrame) {
  log(
    `  first response frame: t=${firstResponseFrame.t.toFixed(0)}ms len=${firstResponseFrame.len}`,
  );
} else {
  log("  first response frame: NEVER");
}
if (firstVisible) {
  log(
    `  first visible bubble text: t=${firstVisible.t.toFixed(0)}ms len=${firstVisible.textLen}`,
  );
} else {
  log("  first visible bubble text: NEVER (live surface never painted text)");
}
if (lastLive) {
  log(`  last live sample: t=${lastLive.t.toFixed(0)}ms textLen=${lastLive.textLen}`);
}

// Final settled state.
await page.waitForTimeout(1500);
const finalText = await page.evaluate(() => {
  const rows = document.querySelectorAll(".message-row.from-agent .message-bubble");
  const last = rows[rows.length - 1];
  return last?.textContent ?? "";
});
log(`  final assistant bubble text length (settled): ${finalText.trim().length}`);

await browser.close();
await stack.stop();
