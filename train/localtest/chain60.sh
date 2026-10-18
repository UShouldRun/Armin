#!/usr/bin/env bash
# Chain 3: the effort template fix (7ea9809) on a binary that has it,
# plus the 60-task set on the best config of each model. Queued like
# chain 2:
#   systemd-run --user --unit=armin-chain3 --working-directory=$PWD \
#     --setenv=CONF=$HOME/.cache/armin-test/xdg \
#     -p StandardOutput=append:$PWD/out/chain3.log \
#     -p StandardError=append:$PWD/out/chain3.log ./chain3.sh
set -u
cd "$(dirname "$0")"
M=/media/henrique/partition/llm/models
K2=$M/K2-Horizon-7B-Q4_K_M.gguf
QW=$M/Qwen3.5-9B-IQ4_XS.gguf
X="--spec-type ngram-mod"
log() { echo "$(date +%H:%M) $*"; }

# one chain on the GPU at a time: wait for the other session's
while systemctl --user is-active -q armin-chain3; do sleep 60; done

# waits out chain 2's runs; run.py itself waits out the user's armin
until ! pgrep -f "run.py --model" > /dev/null; do
  log "chain 2 still running"
  sleep 60
done

export ARMIN=$PWD/armin-fix XDG_CONFIG_HOME=$CONF
run() { log "run $1"; env "${@:3}" python3 run.py --model "$1=$2" > /dev/null; }

# what chain 2's k2-spec-auto (1/28) was missing: armin-new predates
# the template fix
run k2-auto-fix  "$K2" EFFORT=auto EXTRA="$X"

# the 60-task set on the best of each model: k2-spec (25/28) and
# qwen-spec-auto (26/28), each twice
run k2-60        "$K2" EFFORT=high EXTRA="$X" TASKS=all
run qwen-auto-60 "$QW" EFFORT=auto EXTRA="$X" TASKS=all
run k2-60-r2     "$K2" EFFORT=high EXTRA="$X" TASKS=all
run qwen-auto-60-r2 "$QW" EFFORT=auto EXTRA="$X" TASKS=all
python3 run.py --report > /dev/null
log "done: out/REPORT.md"