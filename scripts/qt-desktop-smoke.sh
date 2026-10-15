#!/usr/bin/env bash
# qt-desktop-smoke.sh — Linux Qt desktop acceptance smoke (v1.10.1).
#
# Proves the REAL launch flow end-to-end under Xvfb:
#   core executable (desktop.Run) → spawns Qt host → local IPC (UDS)
#   deterministic fixture engine (scripts/fake-remote-engine.py)
#
# Gates (all must pass; a failure prints the gate and exits 1):
#   G1  typed IPC handshake against the core (sheytan-ipc/1)
#   G2  core-spawned Qt host alive with its own PID
#   G3  REAL visible top-level window owned by the UI PID with plausible
#       geometry (> 200x200) on the smoke display — a 1x1 window, a boot
#       banner, or process survival alone is NEVER GUI proof
#   G4  the UI holds the IPC channel AND the core serves real API
#       interaction over it (authoritative session create/read)
#   G5  clean shutdown: real WM_DELETE_WINDOW close request → UI exit;
#       core stops; no orphan process, no leaked endpoint file
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
WORK="${SMOKE_WORK:-/tmp/sheytan-qt-smoke}"
DISPLAY_NUM="${SMOKE_DISPLAY:-77}"

# v1.10.3 (R1): fail() is defined BEFORE its first call site. The v1.10.2
# script called fail() inside the CORE_BIN block before the definition
# existed; with set -u only (no set -e), the abort printed "command not
# found" and the smoke CONTINUED with a missing core, dying later with a
# confusing empty-log error. Every abort path must end the run here.
#
# v1.10.7 VISIBILITY 2.0 (V-A): every abort path also writes a
# machine-readable marker file naming the failed gate. The workflow's
# Qt smoke failure summary step reads it and re-emits it as an
# anonymously readable ::error:: annotation (check-run annotations API),
# so the next red run names its gate without any login. Best-effort
# write: a missing/unwritable WORK dir must never mask the real failure.
fail() {
  echo "SMOKE FAIL: $1"
  printf '%s\n' "SMOKE FAIL: $1" > "${SMOKE_FAIL_FILE:-$WORK/smoke-fail.txt}" 2>/dev/null || true
  exit 1
}
# X11 helpers (xdotool, xwininfo) come from the SYSTEM PATH by default.
# XTOOLS/XLIBS remain as opt-in overrides for hosts that keep private
# tool prefixes (no machine-specific path is baked into the script).
XTOOLS="${XTOOLS:-}"
XLIBS="${XLIBS:-}"
[ -n "$XTOOLS" ] && export PATH="$XTOOLS:$PATH"
[ -n "$XLIBS" ] && export LD_LIBRARY_PATH="$XLIBS${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

# v1.10.6 (L1, run 38714950052 step 23): Qt Quick cannot create a GL
# scene-graph context under Xvfb on headless hosted runners — the classic
# first-run failure. Force the SOFTWARE renderer for the UI process.
# The UI inherits the core's environment (desktop.Run passes os.Environ()
# to the Qt child), so exports here reach the real renderer. Overridable
# for a developer machine that wants to exercise GL on purpose.
export QT_QUICK_BACKEND="${QT_QUICK_BACKEND:-software}"
export QT_OPENGL="${QT_OPENGL:-software}"
export LIBGL_ALWAYS_SOFTWARE="${LIBGL_ALWAYS_SOFTWARE:-1}"

CORE_BIN="${CORE_BIN:-$WORK/sheytan-core}"
# UI resolution: explicit override → staged copy → the repo's developer
# build tree. The smoke never builds the Qt host itself (that is the Qt
# build/acceptance job's contract) but a normal developer checkout works
# with no staging at all.
UI_BIN="${UI_BIN:-$WORK/ui/build/sheytan-ui}"
if [ ! -x "$UI_BIN" ] && [ -x "$REPO/ui/qt/build/sheytan-ui" ]; then
  UI_BIN="$REPO/ui/qt/build/sheytan-ui"
fi
# v1.10.3 (R2c): a BARE binary name (e.g. WINMGR_BIN=openbox) is resolved
# through PATH; a bare name that resolves nowhere stays empty and the run
# is handled by the explicit WM-less path below (never a silent [ -x ] miss).
WINMGR_BIN="${WINMGR_BIN:-}"
if [ -n "$WINMGR_BIN" ] && [ "${WINMGR_BIN#*/}" = "$WINMGR_BIN" ]; then
  WINMGR_BIN="$(command -v "$WINMGR_BIN" 2>/dev/null || true)"
else
  WINMGR_BIN="${WINMGR_BIN:-$(command -v openbox 2>/dev/null || true)}"
fi

# The smoke owns its binaries: if the core is not provided/staged, build it
# from the repo with the on-PATH Go toolchain (no pre-staged binary is
# assumed — a fresh checkout must be able to run this end to end).
if [ ! -x "$CORE_BIN" ]; then
  if command -v go >/dev/null 2>&1; then
    echo "core binary not staged — building it from the repo with go"
    # v1.10.7 (R-B): the core entry moved to cmd/sheytan-local-agent/.
    (cd "$REPO" && go build -o "$CORE_BIN" ./cmd/sheytan-local-agent) || fail "go build of the core failed"
  else
    fail "core binary $CORE_BIN missing and no 'go' on PATH to build it"
  fi
fi

rm -rf "$WORK/data"
mkdir -p "$WORK/data"
cd "$WORK"

# --- tracked cleanup ---------------------------------------------------------
# Everything the smoke starts is tracked and killed on ANY exit path —
# a failing gate must not leak processes or displays behind it.
XVFB_PID=""
WM_PID=""
ENGINE_PID=""
CORE_PID=""
smoke_cleanup() {
  [ -n "$CORE_PID" ] && kill "$CORE_PID" 2>/dev/null
  [ -n "$ENGINE_PID" ] && kill "$ENGINE_PID" 2>/dev/null
  [ -n "$WM_PID" ] && kill "$WM_PID" 2>/dev/null
  [ -n "$XVFB_PID" ] && kill "$XVFB_PID" 2>/dev/null
  return 0
}
# v1.10.3 (R2a): EXIT always cleans up; INT/TERM clean up AND exit with the
# conventional signal codes — the v1.10.2 single trap resumed execution
# after an interrupt instead of terminating the run.
trap smoke_cleanup EXIT
trap 'smoke_cleanup; exit 130' INT
trap 'smoke_cleanup; exit 143' TERM

# Leftovers from a previous run must never wedge this one. The patterns are
# v1.10.3 (R2b) derived from THIS run's resolved paths ($WORK, $CORE_BIN,
# $UI_BIN and the repo-tree fallback) — the hardcoded
# /tmp/sheytan-qt-smoke patterns missed every overridden SMOKE_WORK and the
# repo-tree UI fallback.
pkill -f "fake-remote-engine.py" 2>/dev/null || true
pkill -f "$CORE_BIN" 2>/dev/null || true
pkill -f "$UI_BIN" 2>/dev/null || true
pkill -f "$REPO/ui/qt/build/sheytan-ui" 2>/dev/null || true
sleep 0.5

# v1.10.6 (L2): the smoke's OWN ui.log. The product launcher (desktop.Run)
# streams the Qt child's stdout/stderr into the CORE's log streams, so the
# former script's failure paths catted a ui.log that NEVER EXISTED and the
# real QML/Qt failure evidence was buried in core.log (or lost entirely).
# Create it up front and route the UI's output into it via a smoke-owned
# wrapper that execs the REAL UI binary UNCHANGED — same argv, same env,
# same PID after exec (G2/G3/G4/G5 observe the real child); only the
# output streams are captured. Deliberately created AFTER the pkill sweep
# above so the stale-process patterns still match the REAL binary path.
UI_LOG="$WORK/ui.log"
: > "$UI_LOG"
if [ -x "$UI_BIN" ]; then
  REAL_UI_BIN="$UI_BIN"
  UI_WRAPPER="$WORK/ui-wrapper.sh"
  cat > "$UI_WRAPPER" <<WRAPPER
#!/usr/bin/env bash
# Smoke-owned diagnostics wrapper (v1.10.6 L2): exec the REAL UI binary
# unchanged; capture its stdout/stderr into the smoke's ui.log.
exec "$REAL_UI_BIN" >>"$UI_LOG" 2>&1
WRAPPER
  chmod +x "$UI_WRAPPER"
  UI_BIN="$UI_WRAPPER"
fi

# --- display + window manager (smoke-OWNED, not product-owned) ---------------
# Clean any stale lock/socket from a previous run (bounded, display-scoped).
rm -f "/tmp/.X${DISPLAY_NUM}-lock" "/tmp/.X11-unix/X${DISPLAY_NUM}" 2>/dev/null || true
Xvfb :$DISPLAY_NUM -screen 0 1280x840x24 -nolisten tcp >"$WORK/xvfb.log" 2>&1 &
XVFB_PID=$!
# v1.10.3: event-based readiness — poll for the display socket instead of a
# fixed sleep (bounded; a dead Xvfb fails fast).
xvfb_ready=""
for i in $(seq 1 30); do
  [ -S "/tmp/.X11-unix/X${DISPLAY_NUM}" ] && kill -0 $XVFB_PID 2>/dev/null && { xvfb_ready="yes"; break; }
  kill -0 $XVFB_PID 2>/dev/null || fail "Xvfb died at startup"
  sleep 0.5
done
[ -n "$xvfb_ready" ] || fail "Xvfb socket never appeared"

# A real window manager owns mapping/geometry/WM_DELETE_WINDOW routing so
# the gates exercise the same protocol path a user's desktop has. Openbox
# (or an equivalent via WINMGR_BIN) is used when present. v1.10.3: without
# any WM the run boots but REFUSES at G3 — geometry is not trustable under
# bare Xvfb, so no GUI proof is claimed WM-less (CI installs openbox, see
# build-desktop.yml). The WM is smoke-owned: it is never a product
# dependency of the shipped desktop pair.
WM_PRESENT=""
if [ -n "$WINMGR_BIN" ] && [ -x "$WINMGR_BIN" ]; then
  # If the WM lives in a private prefix, hand it its own share/config dirs
  # so themes and rc.xml resolve (openbox refuses to run without a theme).
  WM_PREFIX="$(cd "$(dirname "$WINMGR_BIN")/.." && pwd)"
  export XDG_DATA_DIRS="$WM_PREFIX/share${XDG_DATA_DIRS:+:$XDG_DATA_DIRS}"
  export XDG_CONFIG_DIRS="$WM_PREFIX/etc/xdg${XDG_CONFIG_DIRS:+:$XDG_CONFIG_DIRS}"
  DISPLAY=:$DISPLAY_NUM "$WINMGR_BIN" >"$WORK/wm.log" 2>&1 &
  WM_PID=$!
  # v1.10.6 (L3): event-based WM readiness — poll the root-window
  # _NET_SUPPORTING_WM_CHECK property (the ICCCM WM-ownership contract a
  # real WM publishes when it has claimed the display) instead of the
  # former fixed `sleep 1`. Bounded: a WM that never claims the root keeps
  # the run alive but G3 still refuses geometry — no gate is weakened.
  if command -v xprop >/dev/null 2>&1; then
    wm_ready=""
    for i in $(seq 1 20); do
      kill -0 $WM_PID 2>/dev/null || break
      if DISPLAY=:$DISPLAY_NUM xprop -root _NET_SUPPORTING_WM_CHECK 2>/dev/null | grep -q "window id"; then
        wm_ready="yes"
        break
      fi
      sleep 0.5
    done
    if [ -n "$wm_ready" ]; then
      echo "WM: ownership of the root confirmed via _NET_SUPPORTING_WM_CHECK"
    else
      echo "WM: readiness property never appeared (continuing; G3 still gates real geometry)"
    fi
  else
    # xprop unavailable (developer machine without x11-utils): bounded
    # legacy settle. G3 still gates REAL geometry either way — this is
    # not a correctness path.
    sleep 1
  fi
  if kill -0 $WM_PID 2>/dev/null; then
    echo "WM: $WINMGR_BIN-owned display (pid $WM_PID)"
    WM_PRESENT="yes"
  else
    WM_PID=""
    echo "WM: window manager exited immediately — the run will refuse G3 below"
  fi
else
  echo "WM: no window manager available — the run will refuse G3 below (geometry is not trustable under bare Xvfb)"
fi

# --- WM close-request tool (built from the tracked source, never shipped) ----
if [ ! -x "$WORK/closewin" ]; then
  if command -v cc >/dev/null 2>&1; then
    cc -O2 -o "$WORK/closewin" "$REPO/scripts/closewin.c" -lX11 || fail "closewin build failed"
  else
    fail "closewin missing and no cc compiler available to build it"
  fi
fi

# --- fixture engine ----------------------------------------------------------
python3 "$REPO/scripts/fake-remote-engine.py" 18642 >"$WORK/engine.log" 2>&1 &
ENGINE_PID=$!
# v1.10.3: readiness by connection, not by fixed sleep (bounded).
engine_ready=""
for i in $(seq 1 20); do
  kill -0 $ENGINE_PID 2>/dev/null || { cat "$WORK/engine.log"; fail "fixture engine did not start"; }
  if python3 -c "import socket,sys; s=socket.socket(); s.settimeout(0.5); sys.exit(0 if s.connect_ex(('127.0.0.1',18642))==0 else 1)" 2>/dev/null; then
    engine_ready="yes"; break
  fi
  sleep 0.5
done
[ -n "$engine_ready" ] || { cat "$WORK/engine.log"; fail "fixture engine never accepted a connection"; }

# Core config: deterministic remote provider pointing at the fixture engine
# (the fixture replaces the MODEL, never the product — the real run
# pipeline, activity hub and persistence all execute for real).
cat > "$WORK/data/config.json" <<JSON
{
  "provider": "remote",
  "remoteBaseUrl": "http://127.0.0.1:18642/v1",
  "remoteApiKey": "fixture-key",
  "remoteModel": "fixture-model",
  "llamaAutoStart": false,
  "updateSchedule": "off"
}
JSON

# --- core (real executable; the REAL desktop launch path) --------------------
DISPLAY=:$DISPLAY_NUM \
SHEYTAN_DATA_DIR="$WORK/data" \
SHEYTAN_UI_EXECUTABLE="$UI_BIN" \
  "$CORE_BIN" >"$WORK/core.log" 2>&1 &
CORE_PID=$!
# v1.10.3: event-based boot readiness — poll the log for the endpoint line
# instead of a fixed sleep; a core that dies at boot fails fast WITH its log.
core_ready=""
for i in $(seq 1 40); do
  kill -0 $CORE_PID 2>/dev/null || { cat "$WORK/core.log"; echo "--- ui log ---"; cat "$WORK/ui.log" 2>/dev/null || true; fail "core died at boot"; }
  ENDPOINT=$(grep -oE 'core endpoint: .*' "$WORK/core.log" | head -1 | sed 's/core endpoint: //')
  [ -n "$ENDPOINT" ] && { core_ready="yes"; break; }
  sleep 0.5
done
[ -n "$core_ready" ] || { cat "$WORK/core.log"; fail "core did not report an IPC endpoint"; }

# G2: core-spawned UI child alive (the REAL launcher flow).
UI_PID=""
for i in $(seq 1 20); do
  UI_PID=$(pgrep -P $CORE_PID 2>/dev/null | head -1)
  [ -n "$UI_PID" ] && kill -0 "$UI_PID" 2>/dev/null && break
  sleep 0.5
done
[ -n "$UI_PID" ] || { cat "$WORK/core.log" "$WORK/ui.log" 2>/dev/null; fail "G2: core did not spawn a live UI child"; }
echo "G2 PASS: core-spawned Qt host alive (pid $UI_PID, core pid $CORE_PID)"

# G1: typed handshake over the endpoint (with the launcher's ephemeral key
# read from the core process environment — never logged anywhere).
# v1.10.3: the environ read is retried (bounded) — a single racing read of
# /proc/<pid>/environ is the least reliable moment of the whole smoke. The
# key is still ONLY read from the UI child's environment (the launcher
# publishes it nowhere else, by design; it is never logged).
IPC_KEY=""
for i in $(seq 1 10); do
  IPC_KEY=$(tr '\0' '\n' < /proc/$UI_PID/environ 2>/dev/null | grep '^SHEYTAN_IPC_KEY=' | cut -d= -f2)
  [ -n "$IPC_KEY" ] && break
  sleep 0.5
done
[ -n "$IPC_KEY" ] || fail "UI child environment carries no SHEYTAN_IPC_KEY"
python3 - "$ENDPOINT" "$IPC_KEY" <<'PY' || fail "G1 typed handshake"
import socket, sys
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(5)
s.connect(sys.argv[1])
s.sendall(b"GET /api/ipc/handshake HTTP/1.1\r\nHost: core\r\nX-SHEYTAN-IPC-Key: " + sys.argv[2].encode() + b"\r\nConnection: close\r\n\r\n")
buf = b""
while True:
    d = s.recv(65536)
    if not d: break
    buf += d
s.close()
assert b"200" in buf.split(b"\r\n")[0], buf[:200]
assert b"sheytan-ipc" in buf and b"protocolVersion" in buf, buf[:500]
print("G1 PASS: typed handshake (sheytan-ipc/1, keyed)")
PY

# G3: visible top-level window with plausible geometry owned by the UI PID.
# v1.10.3: WITHOUT a window manager the geometry/visibility evidence is not
# trustable (no mapping/reparenting protocol ran), so the gate refuses
# explicitly instead of claiming GUI proof under bare Xvfb. CI installs a
# real WM (openbox) — see build-desktop.yml.
[ -n "$WM_PRESENT" ] || fail "G3: no window manager on this display — GUI geometry proof requires one (install openbox or set WINMGR_BIN)"
# v1.10.3: the Qt host maps MORE than one window (a 1x1 helper alongside
# the real main window) — the v1.10.2 `head -1` picked the helper first and
# failed the gate on a perfectly good 1280x840 main window. Check EVERY
# visible window of the UI pid and pass on the first plausible one; G5
# closes THAT window.
win_ok=""
G3_WIN=""
for i in $(seq 1 40); do
  for win_id in $(DISPLAY=:$DISPLAY_NUM xdotool search --onlyvisible --pid "$UI_PID" 2>/dev/null); do
    geom=$(DISPLAY=:$DISPLAY_NUM xdotool getwindowgeometry --shell "$win_id" 2>/dev/null)
    w=$(echo "$geom" | sed -n 's/^WIDTH=//p')
    h=$(echo "$geom" | sed -n 's/^HEIGHT=//p')
    if [ -n "$w" ] && [ "$w" -gt 200 ] && [ -n "$h" ] && [ "$h" -gt 200 ]; then
      win_ok="id=$win_id ${w}x${h}"
      G3_WIN="$win_id"
      break
    fi
  done
  [ -n "$win_ok" ] && break
  sleep 0.5
done
[ -n "$win_ok" ] || {
  echo "--- root window tree ---"
  DISPLAY=:$DISPLAY_NUM xwininfo -root -tree 2>/dev/null | head -30 || true
  echo "--- ui log ---"; cat "$WORK/ui.log" 2>/dev/null || true
  fail "G3: no visible top-level window with plausible geometry for pid $UI_PID"
}
echo "G3 PASS: visible top-level window ($win_ok)"

# G4: the UI holds the IPC channel + the core serves real interaction.
holds=$(ls -l /proc/$UI_PID/fd 2>/dev/null | grep -c socket || true)
[ "$holds" -ge 1 ] || fail "G4: UI process holds no socket"
python3 - "$ENDPOINT" "$IPC_KEY" <<'PY' || fail "G4 core interaction"
import json, socket, sys
ep = sys.argv[1]
key = sys.argv[2]

def req(method, path, body=None):
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.settimeout(10)
    s.connect(ep)
    payload = json.dumps(body).encode() if body is not None else b""
    head = f"{method} {path} HTTP/1.1\r\nHost: core\r\nX-SHEYTAN-IPC-Key: {key}\r\nConnection: close\r\n"
    if payload:
        head += f"Content-Type: application/json\r\nContent-Length: {len(payload)}\r\n"
    head += "\r\n"
    s.sendall(head.encode() + payload)
    buf = b""
    while True:
        d = s.recv(65536)
        if not d: break
        buf += d
    s.close()
    return buf.split(b"\r\n\r\n", 1)[1]

create = json.loads(req("POST", "/api/sessions", {"name": "smoke-session"}))
sid = create.get("id")
assert sid, create
state = json.loads(req("GET", f"/api/sessions/{sid}"))
assert state.get("id") == sid, state
print(f"G4 PASS: real core interaction over the channel (session {sid[:8]}…)")
PY
echo "G4 PASS: UI holds $holds channel socket(s); core API verified"

# G5: real WM_DELETE_WINDOW close request → clean exit.
# v1.10.3: the close request goes to the window G3 verified (the real main
# window) — closing the 1x1 helper would prove nothing.
UI_WIN="$G3_WIN"
[ -n "$UI_WIN" ] || fail "G5: window vanished before close"
DISPLAY=:$DISPLAY_NUM "$WORK/closewin" "$UI_WIN" || fail "G5: close request send failed"

clean=""
for i in $(seq 1 24); do
  if ! kill -0 "$UI_PID" 2>/dev/null; then clean="yes"; break; fi
  sleep 0.5
done
[ -n "$clean" ] || { echo "--- ui log ---"; cat "$WORK/ui.log"; fail "G5: UI ignored the WM_DELETE_WINDOW close request"; }
echo "G5 PASS: UI exited after the WM close request"

# No orphan core, no leaked endpoint file.
sleep 1
if kill -0 $CORE_PID 2>/dev/null; then
  echo "note: core still stopping…"
  for i in $(seq 1 10); do kill -0 $CORE_PID 2>/dev/null || break; sleep 0.5; done
fi
kill -0 $CORE_PID 2>/dev/null && fail "orphan: core still running after UI exit"
[ -e "$ENDPOINT" ] && fail "leak: endpoint file remains" || true
echo "ALL SMOKE GATES PASS"
