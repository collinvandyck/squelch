#!/usr/bin/env bash
# demo.sh types and runs the commands for the README demo. squelch must be on PATH. Record it with:
#
#   asciinema rec --headless --window-size 100x24 --idle-time-limit 2 --command demo/demo.sh demo.cast
set -u
cd "$(dirname "$0")"
export PATH="$PWD:$PATH"

prompt() {
  printf '\033[1;35m❯\033[0m '
}

comment() {
  prompt
  printf '\033[2m# %s\033[0m\n' "$1"
  sleep 1
}

run() {
  local s=$1
  prompt
  for ((i = 0; i < ${#s}; i++)); do
    printf '%s' "${s:i:1}"
    sleep 0.035
  done
  sleep 0.5
  printf '\n'
  eval "$s"
  sleep 2.5
}

clear
comment "a noisy CLI, run directly"
run "skyctl deploy api"
clear
comment "the same command through squelch"
run "squelch run skyctl deploy api"
comment "if it fails, squelch prints the full stderr and keeps the exit code"
run 'squelch run skyctl deploy api --region=mars-1 || echo "exit code: $?"'
