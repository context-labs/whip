#!/bin/bash
set -euo pipefail

tui_pid=
cleanup() {
    local status=$?
    trap - EXIT INT TERM HUP
    if [[ -n "$tui_pid" ]]; then
        kill -TERM "$tui_pid" 2>/dev/null || true
        wait "$tui_pid" 2>/dev/null || true
    fi
    if (( status != 0 )); then
        whip daemon logs -n 60 >&2 || true
    fi
    whip daemon stop --timeout 5s >&2 || true
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

whip daemon start
whip web --no-open --url http://127.0.0.1:4000 > /dev/null
whip --version
sed -n 's/.*"digest": "\([a-f0-9]*\)".*/Renderer: \1/p' /usr/local/share/whip/renderer-manifest.json
printf '\nWeb: http://localhost:4000 (use a fresh private browser session)\n'
printf 'Workspace: /workspace. Quitting the TUI removes this test environment.\n\n'
# Explicit stdin keeps the background child attached to Docker's terminal while
# wait remains interruptible, so docker stop can run the daemon cleanup trap.
whip <&0 &
tui_pid=$!
status=0
wait "$tui_pid" || status=$?
tui_pid=
exit "$status"
