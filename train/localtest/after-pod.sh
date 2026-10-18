#!/usr/bin/env bash
# The overnight run on the laptop, in order:
#   1. the practical test on the references (base Qwen3-8B, the
#      laptop's Qwen3.5-9B) while the pod still works
#   2. waits for supervise.sh to bring the GGUFs home, tests the 5
#      candidates
#   3. regrades the pod's evals with the current grader, rebuilds its
#      reports (pod/out/REPORT2*.md)
#   4. adds the candidates to armin's config.conf (models.ft-*)
#   5. writes ../MORNING.md: the ranking and how to try them
#
#   setsid nohup ./after-pod.sh > out/after-pod.log 2>&1 < /dev/null &
cd "$(dirname "$0")"
G=$(realpath ../pod/gguf)
LLAMA=/media/henrique/partition/llm/llama.cpp/llama-b11559/llama-server
log() { echo "$(date -u +%H:%M) $*"; }

log "references"
python3 run.py --model base=$G/armin-base-Q4_K_M.gguf \
  --model qwen3.5-9b=/media/henrique/partition/llm/models/Qwen3.5-9B-IQ4_XS.gguf

# supervise.sh logs "downloading out/..." (the full best model) once
# the GGUFs are home; RESTART.md comes after that
start=$(wc -l < ../pod/supervise.log)
until tail -n +$(( start + 1 )) ../pod/supervise.log | grep -q "downloading out/\|pod stopped\|giving up" \
  || { [ -e ../pod/RESTART.md ] && [ ../pod/RESTART.md -nt after-pod.sh ]; }; do
  sleep 120
done
log "GGUFs home: $(ls $G | tr '\n' ' ')"

M=()
for f in $(ls $G/armin-*-Q4_K_M.gguf | grep -v base); do
  n=$(basename $f -Q4_K_M.gguf); M+=( --model ${n#armin-}=$f )
done
log "candidates: ${M[*]}"
python3 run.py "${M[@]}"

# 3. the pod's evals, every run judged by the same (current) grader
log "regrading the pod evals"
N=$(ls -d ../pod/out/rl2/checkpoint-* 2>/dev/null | sed 's/.*-//' | sort -n | tail -1)
cd ..
py() {  # runs a train script on pod/out instead of out/
  python3 -c "import sys, common; common.OUT = 'pod/out'; sys.argv = sys.argv[1:]; \
    import runpy; runpy.run_path(sys.argv[0], run_name = '__main__')" "$@"
}
E=(); for r in base-eval sft-eval rl-eval-179 big-eval r2-eval-$N; do [ -e pod/out/rollouts/$r.jsonl ] && E+=($r); done
X=(); for r in r1-evalx big-evalx r2-evalx-$N; do [ -e pod/out/rollouts/$r.jsonl ] && X+=($r); done
py regrade.py --split eval "${E[@]}"
py regrade.py --split evalx "${X[@]}"
R=(); for r in "${E[@]}"; do [ $r != big-eval ] && R+=($r); done
py report.py --md pod/out/REPORT2.md --rl-log pod/out/rl2 --ref big-eval "${R[@]}"
Y=(); for r in "${X[@]}"; do [ $r != big-evalx ] && Y+=($r); done
py report.py --md pod/out/REPORT2-code.md --by variant --ref big-evalx "${Y[@]}"
cd localtest

# 4. armin's config: one models.ft-<name> per candidate
C=$HOME/.config/armin/config.conf
if ! grep -q "^# --- armin fine-tunes" $C; then
  cp $C $C.pre-ft
  {
    echo
    echo "# --- armin fine-tunes (train/pod/gguf), added $(date -u +%F) for testing ---"
    echo "# /model ft-<name>. Remove this block to drop them (backup: config.conf.pre-ft)."
    p=8111
    for f in $(ls $G/armin-*-Q4_K_M.gguf | grep -v base); do
      n=ft-$(basename $f -Q4_K_M.gguf | sed 's/^armin-//')
      echo "models.$n.binary     = $LLAMA"
      echo "models.$n.model_path = $f"
      echo "models.$n.port       = $p"
      echo "models.$n.args       = --jinja -ngl 999 -c 32768 -lm dio"
      p=$(( p + 1 ))
    done
  } >> $C
fi

# 5. the morning summary
python3 - <<'EOF'
import json, os
rep = open("out/REPORT.md").read()
pod = open("../pod/out/REPORT2.md").read() if os.path.exists("../pod/out/REPORT2.md") else "(no pod report)"
rows = []
for f in sorted(os.listdir("out")):
  if f.endswith(".jsonl"):
    rs = [json.loads(l) for l in open("out/" + f)]
    rows.append((-sum(r["ok"] for r in rs), sum(r["secs"] for r in rs) / max(1, len(rs)), f[:-6], len(rs)))
rank = []
for i, (neg, secs, m, n) in enumerate(sorted(rows), 1):
  ref = m in ("base", "qwen3.5-9b")
  rank.append(f"{i}. **{m}**{' (reference)' if ref else ''}: {-neg}/{n} passed, {secs:.0f} s per task"
    + ("" if ref else f" (try it: `armin` then `/model ft-{m}`)"))
open("../MORNING.md", "w").write(f"""# Overnight results

## Ranking on the practical test (through armin, 28 tasks)

{chr(10).join(rank)}

Details per category and task: localtest/out/REPORT.md, answers and tool
calls per task in localtest/out/<model>.jsonl.

## Trying them

The candidates are in ~/.config/armin/config.conf as models.ft-*
(backup of the old config: config.conf.pre-ft). In armin: `/model
ft-r2-65` (for example); `/model local` goes back to Qwen3.5-9B. GGUFs:
train/pod/gguf/.

## Pod eval (400 tasks, regraded with the current grader)

{pod.split('## Full tables')[0]}
""")
EOF
log "done: ../MORNING.md"
