/* SHEYTAN-LA adaptive web shell controller.
   Owns: workspace registry navigation, screen-class resolution, command
   palette, notification center, and the download-progress surface (engine
   updates: phase, file size done/total, speed). Long work stays behind
   HTTP polling of the SAME loopback API the native shells use; updates
   are coalesced and stop entirely when the surface is hidden (workspace
   lifecycle contract). */
(function () {
  "use strict";

  // ONE registry copy: ids and order match the canonical IA (the Go
  // contract test pins this list against internal/webshell.Workspaces).
  var PRIMARY = [
    { id: "chat", title: "Chat" },
    { id: "agent", title: "Agent" },
    { id: "workspace", title: "Workspace" },
    { id: "lab", title: "Coding Lab" },
    { id: "automation", title: "Automation" },
    { id: "system", title: "System" },
    { id: "settings", title: "Settings" },
    { id: "studio", title: "3D Studio" },
    { id: "engines", title: "Engines" }
  ];
  var UTILITIES = [
    { id: "history", title: "History" },
    { id: "models", title: "Models" },
    { id: "tools", title: "My Tools" },
    { id: "logs", title: "Logs" }
  ];
  var ALL = PRIMARY.concat(UTILITIES);
  var UPDATE_CHECK = "/api/engine/update/check";
  var POLL_ACTIVE_MS = 2000;

  var shell = document.getElementById("shell");
  var stage = document.getElementById("stage");
  var railList = document.getElementById("rail-list");
  var statusDot = document.getElementById("status-dot");
  var statusText = document.getElementById("status-text");
  var notifBtn = document.getElementById("notif-btn");
  var notifPanel = document.getElementById("notifications");
  var notifList = document.getElementById("notif-list");
  var notifEmpty = document.getElementById("notif-empty");
  var notifCount = document.getElementById("notif-count");
  var palette = document.getElementById("palette");
  var paletteInput = document.getElementById("palette-input");
  var paletteList = document.getElementById("palette-list");
  var paletteBtn = document.getElementById("palette-btn");
  var navToggle = document.getElementById("nav-toggle");

  var activeView = null;
  var unread = 0;
  var pollTimer = null;
  var lastDlState = "";
  var paletteIndex = 0;
  var paletteMatches = [];

  function el(tag, cls, text) {
    var node = document.createElement(tag);
    if (cls) { node.className = cls; }
    if (text !== undefined && text !== null) { node.textContent = text; }
    return node;
  }

  function notify(kind, text) {
    var li = el("li", kind, text);
    notifList.appendChild(li);
    notifEmpty.hidden = true;
    unread += 1;
    notifCount.hidden = false;
    notifCount.textContent = String(unread);
    if (notifList.children.length > 50) { notifList.removeChild(notifList.firstChild); }
  }

  /* --- screen class: ONE resolver, applied as data-class on the shell --- */
  function resolveClass(width) {
    if (width < 600) { return "compact"; }
    if (width < 900) { return "medium"; }
    if (width < 1280) { return "expanded"; }
    return "wide";
  }

  function applyClass() {
    var cls = resolveClass(window.innerWidth);
    if (shell.getAttribute("data-class") !== cls) {
      shell.setAttribute("data-class", cls);
      // Compact hides the rail behind the toggle; other classes show it.
      var compact = cls === "compact";
      document.getElementById("rail").hidden = compact && !railOpen;
      navToggle.hidden = !compact;
    }
  }

  var railOpen = false;
  navToggle.addEventListener("click", function () {
    railOpen = !railOpen;
    document.getElementById("rail").hidden = !railOpen;
    navToggle.setAttribute("aria-expanded", String(railOpen));
  });

  /* --- registry navigation: hash routing + Ctrl+number shortcuts --- */
  function buildNav() {
    ALL.forEach(function (w, i) {
      var li = el("li");
      var a = el("a");
      a.href = "#" + w.id;
      a.dataset.view = w.id;
      a.appendChild(el("span", "label", w.title));
      if (i < 9) {
        a.title = w.title + " (Ctrl+" + (i + 1) + ")";
      }
      a.addEventListener("click", function () { railOpen = false; });
      li.appendChild(a);
      railList.appendChild(li);
    });
  }

  function show(id) {
    var known = ALL.some(function (w) { return w.id === id; });
    if (!known) { id = "chat"; }

    if (activeView === id) { return; }

    // Workspace lifecycle: the leaving view stops its timers and listeners.
    stopPolling();

    Array.prototype.forEach.call(stage.querySelectorAll(".view"), function (sec) {
      sec.hidden = sec.getAttribute("data-view") !== id;
    });
    Array.prototype.forEach.call(railList.querySelectorAll("a"), function (a) {
      a.classList.toggle("active", a.dataset.view === id);
    });

    activeView = id;
    stage.scrollTop = 0;
    history.replaceState(null, "", "#" + id);

    if (id === "engines") { startPolling(); }
    if (id === "system") { pollStatus(); }
  }

  /* --- download progress: phase + file size + speed, visible-only --- */
  function humanBytes(n) {
    if (!isFinite(n) || n < 0) { return "—"; }
    var units = ["B", "KiB", "MiB", "GiB", "TiB"];
    var i = 0;
    while (n >= 1024 && i < units.length - 1) { n /= 1024; i += 1; }
    return (i === 0 ? String(n) : n.toFixed(i >= 2 ? 1 : 0)) + " " + units[i];
  }

  function setDot(kind, text) {
    statusDot.className = "dot " + kind;
    statusText.textContent = text;
  }

  function pollStatus() {
    fetch(UPDATE_CHECK, { credentials: "same-origin" })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (data) { renderDownload(data); })
      .catch(function () {
        setDot("err", "core unreachable");
        var core = document.getElementById("core-status");
        if (core) { core.textContent = "the core is unreachable — the API surface is not mounted"; }
      });
  }

  function renderDownload(data) {
    var coreStatus = document.getElementById("core-status");
    var coreDetail = document.getElementById("core-detail");

    if (!data) {
      setDot("warn", "core reachable, update check unavailable");
      if (coreStatus) { coreStatus.textContent = "core reachable; the engine update check route is unavailable in this mode."; }
      return;
    }

    var status = String(data.status || "unknown");
    var msg = String(data.message || "");
    var prog = data.progress || null;

    if (status === "current") { setDot("ok", "engine current"); }
    else if (status === "update-available") { setDot("warn", "engine update available"); }
    else if (status === "installing") { setDot("warn", "engine update running"); }
    else if (status === "offline" || status === "error" || status === "install-failed") { setDot("err", status); }
    else { setDot("ok", status); }

    if (coreStatus) {
      coreStatus.textContent = "engine update status: " + status;
      coreDetail.textContent = msg;
    }

    // Download card: phase, bytes done/total, speed — the file size and
    // status of everything that downloads, in ONE surface.
    var dlStatus = document.getElementById("dl-status");
    var dlBar = document.getElementById("dl-bar");
    var dlFill = document.getElementById("dl-fill");
    var dlProgress = document.getElementById("dl-progress");
    var dlError = document.getElementById("dl-error");
    if (!dlStatus) { return; }

    dlStatus.textContent = msg || status;
    dlStatus.className = "dl-status " + (status === "installing" ? "warn" : (status === "install-failed" || status === "error" ? "err" : "ok"));

    var pct = 0;
    if (prog && prog.Phase === "downloading" && prog.BytesTotal > 0) {
      pct = Math.max(0, Math.min(100, Math.round(prog.BytesDone * 100 / prog.BytesTotal)));
      dlProgress.textContent = "phase " + prog.Phase + " — " + humanBytes(prog.BytesDone) + " of " + humanBytes(prog.BytesTotal) +
        (prog.BytesPerSec > 0 ? " (" + humanBytes(prog.BytesPerSec) + "/s)" : "") + " — " + pct + "%";
    } else if (prog) {
      dlProgress.textContent = "phase " + prog.Phase + " — " + humanBytes(prog.BytesDone) +
        (prog.BytesTotal > 0 ? " of " + humanBytes(prog.BytesTotal) : "");
    } else {
      dlProgress.textContent = "phase — / —";
    }
    dlBar.setAttribute("aria-valuenow", String(pct));
    dlFill.style.width = pct + "%";

    var failed = status === "install-failed" || status === "error";
    dlError.hidden = !failed;
    if (failed) { dlError.textContent = String(data.error || msg || "the last update attempt failed"); }

    // Notification center: phase changes land as events, coalesced.
    var state = status + "|" + (prog ? prog.Phase : "") + "|" + pct;
    if (state !== lastDlState) {
      lastDlState = state;
      if (status === "installing" && prog && prog.Phase === "downloading") {
        notify("warn", "engine update: " + humanBytes(prog.BytesDone) + " of " + humanBytes(prog.BytesTotal));
      } else if (status === "installed") {
        notify("ok", "engine update installed");
      } else if (failed) {
        notify("err", "engine update failed: " + (msg || "see the system view"));
      }
    }
  }

  function startPolling() {
    if (pollTimer !== null) { return; }
    pollStatus();
    pollTimer = window.setInterval(pollStatus, POLL_ACTIVE_MS);
  }

  function stopPolling() {
    if (pollTimer !== null) {
      window.clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  /* --- notification drawer --- */
  notifBtn.addEventListener("click", function () {
    notifPanel.hidden = !notifPanel.hidden;
    notifBtn.setAttribute("aria-expanded", String(!notifPanel.hidden));
    if (!notifPanel.hidden) {
      unread = 0;
      notifCount.hidden = true;
    }
  });

  /* --- command palette (Ctrl/Cmd+K, touch button) --- */
  var COMMANDS = ALL.map(function (w) {
    return { label: "Go to " + w.title, run: function () { location.hash = w.id; show(w.id); } };
  }).concat([
    { label: "Reload status", run: function () { pollStatus(); } },
    { label: "Toggle notifications", run: function () { notifBtn.click(); } }
  ]);

  function renderPalette(query) {
    paletteMatches = COMMANDS.filter(function (c) {
      return c.label.toLowerCase().indexOf(query.toLowerCase()) !== -1;
    }).slice(0, 12);
    paletteList.textContent = "";
    paletteIndex = 0;
    paletteMatches.forEach(function (c, i) {
      var li = el("li", null, c.label);
      li.setAttribute("role", "option");
      li.setAttribute("aria-selected", i === paletteIndex ? "true" : "false");
      li.addEventListener("click", function () { closePalette(); c.run(); });
      paletteList.appendChild(li);
    });
  }

  function openPalette() {
    palette.hidden = false;
    paletteInput.value = "";
    renderPalette("");
    paletteInput.focus();
  }

  function closePalette() {
    palette.hidden = true;
    paletteInput.blur();
  }

  paletteBtn.addEventListener("click", openPalette);
  paletteInput.addEventListener("input", function () { renderPalette(paletteInput.value); });
  paletteInput.addEventListener("keydown", function (e) {
    if (e.key === "Escape") { closePalette(); }
    else if (e.key === "ArrowDown") { movePalette(1); e.preventDefault(); }
    else if (e.key === "ArrowUp") { movePalette(-1); e.preventDefault(); }
    else if (e.key === "Enter" && paletteMatches[paletteIndex]) { closePalette(); paletteMatches[paletteIndex].run(); }
  });
  palette.addEventListener("click", function (e) { if (e.target === palette) { closePalette(); } });

  function movePalette(delta) {
    if (!paletteMatches.length) { return; }
    paletteIndex = Math.max(0, Math.min(paletteMatches.length - 1, paletteIndex + delta));
    Array.prototype.forEach.call(paletteList.children, function (li, i) {
      li.setAttribute("aria-selected", i === paletteIndex ? "true" : "false");
    });
  }

  document.addEventListener("keydown", function (e) {
    if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "K")) { openPalette(); e.preventDefault(); return; }
    if ((e.ctrlKey || e.metaKey) && e.key >= "1" && e.key <= "9") {
      var w = PRIMARY[Number(e.key) - 1];
      if (w) { show(w.id); }
      e.preventDefault();
    }
  });

  /* --- lifecycle: stop work when the surface is hidden --- */
  document.addEventListener("visibilitychange", function () {
    if (document.hidden) { stopPolling(); }
    else if (activeView === "engines") { startPolling(); }
  });

  var resizeTimer = null;
  window.addEventListener("resize", function () {
    if (resizeTimer !== null) { window.clearTimeout(resizeTimer); }
    resizeTimer = window.setTimeout(applyClass, 120);
  });

  /* --- boot: registry → class → route → first meaningful paint --- */
  buildNav();
  applyClass();
  var initial = location.hash.replace("#", "");
  show(ALL.some(function (w) { return w.id === initial; }) ? initial : "chat");
  pollStatus();
})();
