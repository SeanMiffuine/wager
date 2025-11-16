#!/usr/bin/env bash
set -euo pipefail

# resize_and_run.sh
# Usage:
#   ./scripts/resize_and_run.sh [ -r ROWS ] [ -c COLS ] -- <command> [args...]
# Examples:
#   ./scripts/resize_and_run.sh -- go run ./cmd/ui/mockup
#   ./scripts/resize_and_run.sh -r 24 -c 80 -- ./wager

# Defaults
ROWS=24
COLS=80

while getopts ":r:c:" opt; do
    case "$opt" in
        r) ROWS="$OPTARG" ;;
        c) COLS="$OPTARG" ;;
        *) break ;;
    esac
done

shift $((OPTIND - 1))

# Send xterm-compatible request to resize terminal: ESC [ 8 ; rows ; cols t
printf '\033[8;%s;%st' "$ROWS" "$COLS"

# Small pause to let terminal honor the request (if it does)
sleep 0.15

# Query terminal size and report what we see
if size=$(stty size 2>/dev/null || true); then
    if [ -n "$size" ]; then
        rows_act=$(echo "$size" | awk '{print $1}')
        cols_act=$(echo "$size" | awk '{print $2}')
        echo "Requested ${ROWS}x${COLS}. Actual ${rows_act}x${cols_act}."
        if [ "$rows_act" -eq "$ROWS" ] && [ "$cols_act" -eq "$COLS" ]; then
            echo "Resize apparent successful."
        else
            echo "Resize may have been ignored or delayed by terminal preferences." >&2
        fi
    else
        echo "Could not determine terminal size (stty returned empty)." >&2
    fi
else
    echo "Could not determine terminal size (stty failed)." >&2
fi

# Execute the requested command, or fall back to an interactive shell
if [ $# -gt 0 ]; then
    exec "$@"
else
    exec "$SHELL"
fi
