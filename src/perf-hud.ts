// perf-hud.ts — Phase 4 frame-budget diagnostic mode.
//
// A development-only performance HUD that measures real frame timing:
//   - frame time (ms per frame);
//   - dropped frames (frames exceeding 1.5x the budget);
//   - long tasks (PerformanceObserver entry count);
//   - stream update frequency (coalesced setState calls per second).
//
// The HUD is OFF by default. Toggle it with:
//   - window.__shtnTogglePerfHUD() from the dev console;
//   - Ctrl+Shift+P keyboard shortcut.
//
// What this is NOT:
//   - this is NOT a guaranteed 120 FPS claim;
//   - this is NOT a production profiler (it adds small overhead);
//   - the HUD's own measurements are real (Performance API), but the
//     target budget is a guideline (8.33 ms for 120 Hz, 16.67 ms for
//     60 Hz — the HUD auto-detects from the display's refresh rate).
//
// Frame-budget engineering (Phase 4 spec §19): the HUD exists so a
// developer can verify that streaming coalescing actually reduces React
// updates per second, and that animations stay on compositor-friendly
// properties.

declare global {
  interface Window {
    __shtnTogglePerfHUD?: () => void;
    __shtnPerfHUD?: PerfHUD;
  }
}

const STORAGE_KEY = "shtn:perf-hud-enabled";

// Stream update counter: incremented by the store's flushStreaming path
// so the HUD can measure coalesced update frequency.
let streamUpdateCount = 0;

export function recordStreamUpdate(): void {
  streamUpdateCount += 1;
}

// Long task observer: counts any task > 50ms (the standard longtask
// threshold). Held outside the HUD instance so it survives toggling.
let longTaskCount = 0;
let longTaskObserver: PerformanceObserver | null = null;

function ensureLongTaskObserver(): void {
  if (longTaskObserver !== null) return;
  if (typeof PerformanceObserver === "undefined") return;

  try {
    longTaskObserver = new PerformanceObserver((list) => {
      longTaskCount += list.getEntries().length;
    });
    longTaskObserver.observe({ entryTypes: ["longtask"] });
  } catch {
    // longtask is not supported on every browser; the HUD reports 0
    // honestly when unsupported.
    longTaskObserver = null;
  }
}

export class PerfHUD {
  private rafId: number | null = null;
  private overlay: HTMLDivElement | null = null;
  private lastFrameTime = 0;
  private frameTimes: number[] = [];
  private readonly maxFrameSamples = 120;

  // Session counters (reset every display tick).
  private sessionStartTime = 0;
  private sessionFrameCount = 0;
  private sessionDroppedFrames = 0;
  private sessionStreamUpdatesAtStart = 0;

  // Smoothed metrics for display.
  private displayRefreshHz = 60;
  private targetFrameBudgetMs = 16.67;

  public enabled = false;

  constructor() {
    // Detect display refresh rate via requestAnimationFrame timing.
    // Falls back to 60 Hz when rAF is unavailable.
    this.detectRefreshRate();
  }

  private detectRefreshRate(): void {
    if (typeof requestAnimationFrame === "undefined") return;

    let count = 0;
    let first = 0;
    let last = 0;

    const measure = (t: number) => {
      if (first === 0) {
        first = t;
      } else {
        last = t;
        count += 1;
      }

      if (count < 10) {
        requestAnimationFrame(measure);
      } else {
        const elapsed = last - first;
        if (elapsed > 0 && count > 1) {
          const fps = (count - 1) / (elapsed / 1000);
          this.displayRefreshHz = Math.round(fps);
          this.targetFrameBudgetMs = 1000 / this.displayRefreshHz;
        }
      }
    };

    requestAnimationFrame(measure);
  }

  enable(): void {
    if (this.enabled) return;

    this.enabled = true;
    ensureLongTaskObserver();

    this.overlay = document.createElement("div");
    this.overlay.style.position = "fixed";
    this.overlay.style.bottom = "12px";
    this.overlay.style.right = "12px";
    this.overlay.style.zIndex = "99999";
    this.overlay.style.padding = "8px 12px";
    this.overlay.style.background = "rgba(0, 0, 0, 0.82)";
    this.overlay.style.color = "#0F0";
    this.overlay.style.fontFamily = "monospace";
    this.overlay.style.fontSize = "11px";
    this.overlay.style.lineHeight = "1.45";
    this.overlay.style.borderRadius = "6px";
    this.overlay.style.pointerEvents = "none";
    this.overlay.style.userSelect = "none";
    this.overlay.style.letterSpacing = "0.02em";
    this.overlay.style.minWidth = "220px";
    this.overlay.style.whiteSpace = "pre";
    document.body.appendChild(this.overlay);

    this.sessionStartTime = performance.now();
    this.sessionStreamUpdatesAtStart = streamUpdateCount;
    this.frameTimes.length = 0;
    this.sessionFrameCount = 0;
    this.sessionDroppedFrames = 0;

    this.lastFrameTime = performance.now();
    this.tick();

    try {
      localStorage.setItem(STORAGE_KEY, "1");
    } catch {
      // localStorage may be unavailable (private mode); ignore.
    }
  }

  disable(): void {
    this.enabled = false;

    if (this.rafId !== null) {
      cancelAnimationFrame(this.rafId);
      this.rafId = null;
    }

    if (this.overlay !== null && this.overlay.parentNode) {
      this.overlay.parentNode.removeChild(this.overlay);
    }

    this.overlay = null;

    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch {
      // ignore
    }
  }

  toggle(): void {
    if (this.enabled) {
      this.disable();
    } else {
      this.enable();
    }
  }

  private tick = (): void => {
    if (!this.enabled || this.overlay === null) return;

    const now = performance.now();
    const delta = now - this.lastFrameTime;
    this.lastFrameTime = now;

    this.frameTimes.push(delta);
    if (this.frameTimes.length > this.maxFrameSamples) {
      this.frameTimes.shift();
    }

    this.sessionFrameCount += 1;

    // A frame is "dropped" if it exceeds 1.5x the budget (standard
    // browser definition of a jank frame).
    if (delta > this.targetFrameBudgetMs * 1.5) {
      this.sessionDroppedFrames += 1;
    }

    // Update the overlay every ~10 frames (so the HUD itself doesn't
    // contribute to jank by re-rendering every frame).
    if (this.sessionFrameCount % 10 === 0) {
      this.render();
    }

    this.rafId = requestAnimationFrame(this.tick);
  };

  private render(): void {
    if (this.overlay === null) return;

    const samples = this.frameTimes.length;
    if (samples === 0) return;

    const sum = this.frameTimes.reduce((a, b) => a + b, 0);
    const avgFrame = sum / samples;
    const maxFrame = Math.max(...this.frameTimes);
    const minFrame = Math.min(...this.frameTimes);

    const sessionElapsed = (performance.now() - this.sessionStartTime) / 1000;
    const fps = sessionElapsed > 0
      ? (this.sessionFrameCount / sessionElapsed).toFixed(1)
      : "0.0";
    const dropPct = this.sessionFrameCount > 0
      ? ((this.sessionDroppedFrames / this.sessionFrameCount) * 100).toFixed(1)
      : "0.0";

    const streamUpdates = streamUpdateCount - this.sessionStreamUpdatesAtStart;
    const streamHz = sessionElapsed > 0
      ? (streamUpdates / sessionElapsed).toFixed(1)
      : "0.0";

    const budget = this.targetFrameBudgetMs.toFixed(2);

    this.overlay.textContent =
      `SHEYTAN perf HUD\n` +
      `display   ${this.displayRefreshHz} Hz (budget ${budget} ms)\n` +
      `frame     avg ${avgFrame.toFixed(2)}  min ${minFrame.toFixed(2)}  max ${maxFrame.toFixed(2)} ms\n` +
      `fps       ${fps}\n` +
      `dropped   ${this.sessionDroppedFrames} (${dropPct}%)\n` +
      `longtask  ${longTaskCount}\n` +
      `stream    ${streamHz} updates/s (coalesced)\n` +
      `[Ctrl+Shift+P to toggle]`;
  }
}

// Singleton instance + window binding for dev console access.
let hudInstance: PerfHUD | null = null;

export function initPerfHUD(): void {
  if (hudInstance !== null) return;
  if (typeof window === "undefined") return;

  hudInstance = new PerfHUD();
  window.__shtnPerfHUD = hudInstance;
  window.__shtnTogglePerfHUD = () => hudInstance?.toggle();

  // Restore from localStorage (dev convenience: stays on across reloads
  // while you're iterating on perf).
  let restore = false;
  try {
    restore = localStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    // ignore
  }

  if (restore) {
    hudInstance.enable();
  }

  // Keyboard shortcut: Ctrl+Shift+P.
  window.addEventListener("keydown", (e) => {
    if (e.ctrlKey && e.shiftKey && (e.key === "P" || e.key === "p")) {
      e.preventDefault();
      hudInstance?.toggle();
    }
  });
}

// recordStreamUpdate is the public hook the store calls on every
// coalesced streaming flush. The store imports this directly (it does
// NOT import the perf-hud class, keeping the dep one-way).
// (Already exported at its declaration above.)
