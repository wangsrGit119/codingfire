#!/usr/bin/env bash
#
# Bring the Linux build up and check that it actually works.
#
# This is the only automated check of the X11 window layer. The overlay has to
# find its own window by _NET_WM_PID, apply the EWMH overlay styles to it, and
# position it — none of which is observable from a compiler. So the log is the
# assertion, and the assertion is positive: the app announces the placement only
# after all three have succeeded. See the wait_for_log call below.
#
# Run it under a virtual X server:
#
#   xvfb-run -a -s "-screen 0 1280x800x24" scripts/linux-smoke.sh dist/CodingFire
#
# The headless half needs no display at all and is worth running on its own.

set -euo pipefail

binary=${1:-dist/CodingFire}
if [[ ! -x "$binary" ]]; then
    echo "not executable: $binary" >&2
    exit 1
fi

workdir=$(mktemp -d)
# CODINGFIRE_DATA_DIR keeps the run out of the real user's data, and gives the
# log a known path to assert against.
export CODINGFIRE_DATA_DIR="$workdir/data"
log="$CODINGFIRE_DATA_DIR/codingfire.log"
dump="$workdir/dump.txt"

dump_log() {
    if [[ -f "$log" ]]; then
        echo "--- $log ---" >&2
        cat "$log" >&2
    else
        echo "--- no log written ---" >&2
    fi
}

# ---------------------------------------------------------------------------
# Headless: the data layer has to work on Linux at all
# ---------------------------------------------------------------------------

echo "== --dump =="
"$binary" --dump "$dump"
head -3 "$dump"

grep -q '^CodingFire ' "$dump" || {
    echo "FAIL: the dump has no header line" >&2
    exit 1
}
grep -q '^sources$' "$dump" || {
    echo "FAIL: the dump has no sources section" >&2
    exit 1
}
# The report lists one line per collected source. A Linux run that collected
# nothing would still produce a well-formed report, so the count is what makes
# this a real check.
sources=$(grep -c '^  [A-Za-z]' "$dump" || true)
echo "sources listed: $sources"
if [[ "$sources" -lt 10 ]]; then
    echo "FAIL: expected the full source list, got $sources" >&2
    cat "$dump" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# GUI under the virtual display
# ---------------------------------------------------------------------------

if [[ -z "${DISPLAY:-}" ]]; then
    echo "== GUI skipped: no DISPLAY =="
    exit 0
fi

echo "== GUI on $DISPLAY =="
"$binary" &
app_pid=$!

cleanup() {
    kill "$app_pid" 2>/dev/null || true
    wait "$app_pid" 2>/dev/null || true
}
trap cleanup EXIT

# Poll rather than sleep a fixed amount: a slow first frame should not need a
# generous timeout, and a hang is caught as early as a crash.
appeared=0
for _ in $(seq 1 60); do
    if ! kill -0 "$app_pid" 2>/dev/null; then
        break # it died; reported below
    fi
    if xwininfo -root -tree 2>/dev/null | grep -q '"CodingFire"'; then
        appeared=1
        break
    fi
    sleep 0.5
done

if ! kill -0 "$app_pid" 2>/dev/null; then
    echo "FAIL: the app exited before its window appeared" >&2
    dump_log
    exit 1
fi

if [[ "$appeared" != 1 ]]; then
    echo "FAIL: no window titled CodingFire appeared within 30s" >&2
    echo "--- window tree ---" >&2
    xwininfo -root -tree 2>&1 | head -40 >&2 || true
    dump_log
    exit 1
fi

echo "window appeared"

# wait_for_log polls for a line rather than grepping once: X11 maps the window
# during backend New(), before OnInit has run, so the log trails the window by a
# moment.
wait_for_log() {
    local pattern=$1
    for _ in $(seq 1 80); do
        grep -q "$pattern" "$log" 2>/dev/null && return 0
        sleep 0.25
    done
    return 1
}

# Positive proof that the window layer engaged, rather than merely that a window
# exists. This line is written by placeDefault only after it has found our window
# by _NET_WM_PID, read the work area through RandR, and moved the window with
# ConfigureWindow — the three things the X11 shim exists to do. A fresh data
# directory has no saved position, so this path always runs on first launch.
#
# It also makes the absence checks below sound: the warning they look for is
# logged earlier in the same call, so once this line is present any "not found"
# warning is already in the file. Without it, "the log has no warning in it"
# would also be satisfied by a log that was never written.
if ! wait_for_log 'campfire placed at'; then
    echo "FAIL: the window layer never placed the campfire" >&2
    dump_log
    exit 1
fi
echo "window layer engaged: $(grep 'campfire placed at' "$log" | tail -1)"

# A window existing is not the same as the app having found it: this is the
# line applyPlatformTweaks logs when FindOwnWindowByTitle came back empty.
if grep -q 'campfire window not found' "$log"; then
    echo "FAIL: the app could not find its own window" >&2
    dump_log
    exit 1
fi

# The hover card is a second window with its own OnInit; it is parked off-screen
# rather than closed, so a failure to find it is silent everywhere else.
if grep -q 'hover card window not found' "$log"; then
    echo "FAIL: the app could not find its hover-card window" >&2
    dump_log
    exit 1
fi

# The tray needs a StatusNotifier host on the session bus. A container has
# neither, so its absence is expected here and must not be treated as a failure
# — but it is worth showing, because it is the first thing to check when the app
# comes up without a tray on a real desktop.
if grep -q 'tray unavailable' "$log" 2>/dev/null; then
    echo "note: no tray in this environment (expected without a session bus):"
    grep 'tray unavailable' "$log" >&2 || true
fi

# Still alive after all of that, which is what rules out a panic in the tick
# loop or the scan callbacks.
if ! kill -0 "$app_pid" 2>/dev/null; then
    echo "FAIL: the app exited after its window appeared" >&2
    dump_log
    exit 1
fi

echo "== ok =="
