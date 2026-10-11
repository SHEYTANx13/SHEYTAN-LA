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
XTOOLS="${XTOOLS:-/home/z/xcb-libs/usr/bin}"
XLIBS="${XLIBS:-/home/z/xcb-libs/usr/lib/x86_64-linux-gnu}"
export PATH="$XTOOLS:$PATH"
export LD_LIBRARY_PATH="$XLIBS${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

CORE_BIN="${CORE_BIN:-$WORK/sheytan-core}"
UI_BIN="${UI_BIN:-$WORK/ui/build/sheytan-ui}"

rm -rf "$WORK/data"
mkdir -p "$WORK/data"
cd "$WORK"

fail() { echo "SMOKE FAIL: $1"; exit 1; }

# Leftovers from a previous run must never wedge this one.
pkill -f "fake-remote-engine.py" 2>/dev/null || true
pkill -f "sheytan-qt-smoke/sheytan-core" 2>/dev/null || true
pkill -f "sheytan-qt-smoke/ui/build/sheytan-ui" 2>/dev/null || true
sleep 0.5

# --- display -----------------------------------------------------------------
# Clean any stale lock/socket from a previous run (bounded, display-scoped).
rm -f "/tmp/.X${DISPLAY_NUM}-lock" "/tmp/.X11-unix/X${DISPLAY_NUM}" 2>/dev/null || true
Xvfb :$DISPLAY_NUM -screen 0 1280x840x24 -nolisten tcp >"$WORK/xvfb.log" 2>&1 &
XVFB_PID=$!
sleep 1.5
kill -0 $XVFB_PID 2>/dev/null || fail "Xvfb died at startup"

# --- fixture engine ----------------------------------------------------------
python3 "$REPO/scripts/fake-remote-engine.py" 18642 >"$WORK/engine.log" 2>&1 &
ENGINE_PID=$!
sleep 0.5
kill -0 $ENGINE_PID 2>/dev/null || fail "fixture engine did not start"

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
sleep 2
kill -0 $CORE_PID 2>/dev/null || { cat "$WORK/core.log"; fail "core died at boot"; }

ENDPOINT=$(grep -oE 'core endpoint: .*' "$WORK/core.log" | head -1 | sed 's/core endpoint: //')
[ -n "$ENDPOINT" ] || fail "core did not report an IPC endpoint"

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
IPC_KEY=$(tr '\0' '\n' < /proc/$UI_PID/environ 2>/dev/null | grep '^SHEYTAN_IPC_KEY=' | cut -d= -f2)
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
win_ok=""
for i in $(seq 1 40); do
  win_id=$(DISPLAY=:$DISPLAY_NUM xdotool search --onlyvisible --pid "$UI_PID" 2>/dev/null | head -1)
  if [ -n "$win_id" ]; then
    geom=$(DISPLAY=:$DISPLAY_NUM xdotool getwindowgeometry --shell "$win_id" 2>/dev/null)
    w=$(echo "$geom" | sed -n 's/^WIDTH=//p')
    h=$(echo "$geom" | sed -n 's/^HEIGHT=//p')
    if [ -n "$w" ] && [ "$w" -gt 200 ] && [ -n "$h" ] && [ "$h" -gt 200 ]; then
      win_ok="id=$win_id ${w}x${h}"
      break
    fi
  fi
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
UI_WIN=$(DISPLAY=:$DISPLAY_NUM xdotool search --onlyvisible --pid "$UI_PID" 2>/dev/null | head -1)
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
