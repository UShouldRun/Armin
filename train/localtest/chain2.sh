#!/usr/bin/env bash
# Qwen3.5-9B against K2 Horizon 7B on equal terms, after chain.sh:
# same armin, same config copy, ngram-mod for both, each run twice.
set -u
cd "$(dirname "$0")"
M=/media/henrique/partition/llm/models
K2=$M/K2-Horizon-7B-Q4_K_M.gguf
QW=$M/Qwen3.5-9B-IQ4_XS.gguf
X="--spec-type ngram-mod"
log() { echo "$(date +%H:%M) $*"; }

while systemctl --user is-active -q armin-chain; do sleep 30; done

export ARMIN=$PWD/armin-new XDG_CONFIG_HOME=$CONF
run() { log "run $1"; env "${@:3}" python3 run.py --model "$1=$2" > /dev/null; }
run qwen-spec      "$QW" EFFORT=high EXTRA="$X"
run qwen-spec-auto "$QW" EFFORT=auto EXTRA="$X"
run k2-spec-r2     "$K2" EFFORT=high EXTRA="$X"
run qwen-spec-r2   "$QW" EFFORT=high EXTRA="$X"
python3 run.py --report > /dev/null
log "done: out/REPORT.md"
