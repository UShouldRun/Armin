#!/usr/bin/env bash
# The speed proposals for K2 Horizon 7B, one after another:
#   1. specbench.py: llama-server options timed on armin-like work
#   2. the practical test with the fastest n-gram speculation, alone
#      and combined with a q4_0 KV cache, the IQ4_XS quant, and
#      effort auto (each compared with run 1's settings)
# Runs on armin-new with a config copy that has no library, so only
# the option under test changes. Waits for the GPU (run.py does).
#
#   CONF=/path/to/xdg setsid ./chain.sh > out/chain.log 2>&1
set -u
cd "$(dirname "$0")"
M=/media/henrique/partition/llm/models
Q4=$M/K2-Horizon-7B-Q4_K_M.gguf
IQ=$M/K2-Horizon-7B-IQ4_XS.gguf
log() { echo "$(date +%H:%M) $*"; }

while systemctl --user is-active -q armin-k2test2; do sleep 30; done
until [ -s "$IQ" ] && ! pgrep -f "K2-Horizon-7B-IQ4_XS.gguf" > /dev/null; do sleep 30; done

log "speed benchmark"
python3 specbench.py "$Q4" "$IQ"

# the n-gram mode with the best copy-heavy speed, if it beats none
spec=$(python3 - <<'EOF'
import re
best, best_tps = "none", 0
for line in open("out/SPEED.md"):
  f = [ c.strip() for c in line.split("|") ]
  if len(f) > 5 and f[2] == "q8_0" and f[3].startswith(("none", "ngram")):
    m = re.match(r"([\d.]+) tok/s", f[4])
    if m and float(m[1]) > best_tps:
      best, best_tps = f[3], float(m[1])
print(best)
EOF
)
log "fastest speculation: $spec"
X=""
[ "$spec" != none ] && X="--spec-type $spec"

export ARMIN=$PWD/armin-new XDG_CONFIG_HOME=$CONF
run() { log "run $1"; env "${@:3}" python3 run.py --model "$1=$2" > /dev/null; }
run k2-spec      "$Q4" EFFORT=high EXTRA="$X"
run k2-spec-kvq4 "$Q4" EFFORT=high EXTRA="$X" KV=q4_0
run k2-spec-iq4  "$IQ" EFFORT=high EXTRA="$X"
run k2-spec-auto "$Q4" EFFORT=auto EXTRA="$X"
python3 run.py --report > /dev/null
log "done: out/REPORT.md, out/SPEED.md"
