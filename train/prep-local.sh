#!/usr/bin/env bash
# Runs on the pod beside round2.sh: turns the candidate models into
# Q4_K_M GGUFs (out/gguf/) for the laptop's practical test.
#
#   setsid nohup ./prep-local.sh > /workspace/armin/prep.log 2>&1 < /dev/null &
#
# Converts the models that already exist right away, then, once round 2
# is done, merges RL checkpoint 50, averages round 1 with round 2's
# final model and converts those. /workspace/armin/.prep holds the
# pod's stop (pod-guard.sh's runpodctl) until it is done.
set -u
W=/workspace/armin
cd $W/train
LL=/root/llama.cpp
PY=.venv/bin/python
touch $W/.prep
trap 'rm -f $W/.prep; log "prep done"' EXIT

log() { echo "$(date -u +%H:%M) $*"; }

# the laptop's llama.cpp build, so its llama-server reads these files
if [ ! -x $LL/build/bin/llama-quantize ]; then
  git clone -q --depth 1 --branch b11559 https://github.com/ggml-org/llama.cpp $LL
  cmake -S $LL -B $LL/build -DGGML_CUDA=OFF -DLLAMA_CURL=OFF -DCMAKE_BUILD_TYPE=Release > /dev/null
  nice cmake --build $LL/build -j 6 --target llama-quantize > /dev/null || { log "llama.cpp build failed"; exit 1; }
fi

mkdir -p out/gguf
gguf() {  # gguf <model dir> <name>
  local out=out/gguf/armin-$2-Q4_K_M.gguf
  [ -e $out ] && return
  [ -e $1/config.json ] || { log "no $1, skipping $2"; return; }
  log "converting $1 -> $out"
  PYTHONPATH=$LL/gguf-py nice $PY $LL/convert_hf_to_gguf.py $1 --outtype bf16 \
    --outfile /dev/shm/$2.gguf > /dev/null \
    && nice $LL/build/bin/llama-quantize /dev/shm/$2.gguf $out.part Q4_K_M > /dev/null \
    && mv $out.part $out || log "conversion of $2 failed"
  rm -f /dev/shm/$2.gguf
}

gguf out/rl-merged-179 r1-179
gguf out/sft2-merged r2-sft

until grep -qE "^== (report|stopping pod)" $W/round2.log || ! pgrep -f "[r]ound2.sh" > /dev/null; do
  sleep 60
done
log "round 2 done"

N=$(ls -d out/rl2/checkpoint-* 2>/dev/null | sed 's/.*-//' | sort -n | tail -1)
for n in 50 $N; do
  [ -e out/rl2/checkpoint-$n ] && [ ! -e out/r2-merged-$n/config.json ] \
    && $PY merge.py --base out/sft2-merged --adapter out/rl2/checkpoint-$n --out out/r2-merged-$n
done

# round 1 and round 2 averaged: keeps round 2's new habits with less
# of the forgetting, if any
if [ -n "$N" ] && [ ! -e out/avg-r1-r2/config.json ]; then
  $PY - "$N" <<'EOF'
import sys, torch
from transformers import AutoModelForCausalLM, AutoTokenizer
a = AutoModelForCausalLM.from_pretrained("out/rl-merged-179", dtype = torch.bfloat16)
b = AutoModelForCausalLM.from_pretrained(f"out/r2-merged-{sys.argv[1]}", dtype = torch.bfloat16)
sb = b.state_dict()
with torch.no_grad():
  for k, v in a.state_dict().items():
    v.copy_(((v.float() + sb[k].float()) / 2).to(v.dtype))
a.save_pretrained("out/avg-r1-r2")
AutoTokenizer.from_pretrained("out/rl-merged-179").save_pretrained("out/avg-r1-r2")
EOF
fi

[ "$N" != 50 ] && gguf out/r2-merged-50 r2-50
gguf out/r2-merged-$N r2-$N
gguf out/avg-r1-r2 avg-r1-r2
ls -la out/gguf
touch out/gguf/.ready
