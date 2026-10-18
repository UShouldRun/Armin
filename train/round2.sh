#!/usr/bin/env bash
# Round 2, on top of run.sh's result: the multi-hop search habit and
# the science workflow (README.md, "Round 2"). Like run.sh, a stage
# whose output exists is skipped, so rerun it after an interruption.
#
#   BUDGET_HOURS=14 AUTO_STOP=1 ./round2.sh
#   STEPS=300 ./round2.sh          # later: continue round-2 RL
#   MID_EVAL=1 ./round2.sh         # also eval right after the warm-up
#
# The bigger model (BIG, Qwen3.8-27B) is evaluated on the same tasks
# as the target, and its grounded successes on the sft split are the
# warm-up's math, science and code episodes. Ends with out/REPORT2.md:
# base, round 1, round 2 and the bigger model side by side.
set -euo pipefail
cd "$(dirname "$0")"

export ARMIN_BASE="${ARMIN_BASE:-Qwen/Qwen3-8B}"
export VLLM_USE_FLASHINFER_SAMPLER=0
# DeepGEMM (FP8) JIT-compiles with nvcc >= 12.9; the pod has 12.8.
export VLLM_USE_DEEP_GEMM=0
PY="${PY:-.venv/bin/python}"
LIMIT_EVAL="${LIMIT_EVAL:-400}"
BIG="${BIG:-Qwen/Qwen3.8-27B-FP8}"
LIMIT_TEACHER="${LIMIT_TEACHER:-1000}"  # sft tasks the bigger model works
GENS="${GENS:-12}"        # tries per question; 12 x 3 costs about 8 x 4
PROMPTS="${PROMPTS:-3}"
START=$(date +%s)
RESERVE=4500              # seconds kept after RL for merge, eval and report

stop_pod() {
  if [ "${AUTO_STOP:-0}" = 1 ] && [ -n "${RUNPOD_POD_ID:-}" ]; then
    echo "== stopping pod $RUNPOD_POD_ID"
    runpodctl stop pod "$RUNPOD_POD_ID"
  fi
}

trap stop_pod EXIT
DEADLINE=0
if [ -n "${BUDGET_HOURS:-}" ]; then
  DEADLINE=$(python3 -c "print(int($START + $BUDGET_HOURS * 3600 - $RESERVE))")
  STEPS="${STEPS:-2000}"
fi

STEPS="${STEPS:-200}"

stage() {  # stage <output that marks it done> <command...>
  local done="$1"; shift
  if [ -e "$done" ]; then
    echo "-- skip, $done exists"
    return
  fi

  echo "== $*"
  local rc=0
  "$@" || rc=$?
  pkill -9 -f "VLLM::EngineCore" 2>/dev/null || true
  return "$rc"
}

roll() {  # roll <name> <model> [rollout.py args...]
  local name="$1" model="$2"; shift 2
  stage "out/rollouts/$name.jsonl" "$PY" rollout.py --model "$model" \
    --split eval --mode free --n 1 --limit "$LIMIT_EVAL" --out "$name" "$@"
}

# Round 2's code tasks (hard problems, planted bugs) have their own
# eval split, so round 1's eval stays exactly as it was.
rollx() {  # rollx <name> <model> [rollout.py args...]
  local name="$1" model="$2"; shift 2
  stage "out/rollouts/$name.jsonl" "$PY" rollout.py --model "$model" \
    --split evalx --mode free --n 1 --out "$name" "$@"
}

# Round 1's final model: the merged RL checkpoint with the most steps.
R1=$(ls -d out/rl-merged-* 2>/dev/null | sed 's/.*-//' | sort -n | tail -1)
if [ -z "$R1" ]; then
  echo "no out/rl-merged-N; run run.sh first" >&2
  exit 1
fi

# Same splits and ids as round 1; adds the search hops and the
# science library to the corpus.
prepare() {
  "$PY" prepare.py && touch data/.round2
}

stage data/.round2 prepare
"$PY" check.py --split eval --per-kind 10
stage data/rl2.labeled.jsonl "$PY" label.py --split rl \
  --rollouts base-rl-nt --extra rlx --out rl2.labeled
# Round 1's model on the new code tasks: the "before" for them.
rollx r1-evalx "out/rl-merged-$R1"

# The target, on the same tasks and prompt as round 2's eval, then
# its episodes to imitate (memory, skill and save are solved). If the
# bigger model fails to load, round 2 goes on without it.
roll big-eval "$BIG" --text-only || echo "-- $BIG eval failed; no reference"
rollx big-evalx "$BIG" --text-only || echo "-- $BIG code eval failed"
stage out/rollouts/big-sft.jsonl "$PY" rollout.py --model "$BIG" --text-only \
  --split sft --kinds math,science,search,code --mode free --n 1 \
  --limit "$LIMIT_TEACHER" --out big-sft || echo "-- $BIG teacher failed"
stage out/rollouts/big-sftx.jsonl "$PY" rollout.py --model "$BIG" --text-only \
  --split sftx --mode free --n 1 --out big-sftx || echo "-- $BIG code teacher failed"

# Teacher episodes improved off the pod (a guided rerun of the failed
# ones) arrive with out/rollouts/.teacher-ready; wait for them up to
# TEACHER_WAIT minutes, logging so pod-guard.sh sees it alive.
if [ "${TEACHER_WAIT:-0}" -gt 0 ] && [ ! -e data/sft_r2.jsonl ]; then
  for i in $(seq 1 "$TEACHER_WAIT"); do
    [ -e out/rollouts/.teacher-ready ] && break
    echo "-- waiting for the teacher episodes ($i min)"
    sleep 60
  done
fi

TEACHER=()
T=""
for t in big-sft big-sftx; do
  [ -e "out/rollouts/$t.jsonl" ] && T="${T:+$T,}$t"
done
[ -n "$T" ] && TEACHER=(--teacher "$T")
stage data/sft_r2.jsonl "$PY" build_sft.py --topup "${TEACHER[@]}" \
  --replay sft_train --out sft_r2

# A short, gentle warm-up: new habits without undoing round 1.
stage out/sft2/adapter_config.json "$PY" sft.py --model "out/rl-merged-$R1" \
  --data sft_r2 --out out/sft2 --epochs 1 --lr 5e-5
stage out/sft2-merged/config.json "$PY" merge.py --base "out/rl-merged-$R1" \
  --adapter out/sft2 --out out/sft2-merged
if [ "${MID_EVAL:-0}" = 1 ]; then
  roll r2-sft-eval out/sft2-merged
fi

rl_until() {
  if [ "$DEADLINE" != 0 ] && [ $(( DEADLINE - $(date +%s) )) -lt 900 ]; then
    echo "-- no time left for RL before the deadline"
    return
  fi

  "$PY" rl.py --model out/sft2-merged --tasks rl2.labeled --out out/rl2 \
    --steps "$STEPS" --gens "$GENS" --prompts "$PROMPTS" \
    --search-weight 2 --curriculum --deadline "$DEADLINE" --resume \
    || echo "-- rl.py exited with $?; using its last checkpoint"
  if [ "$(cat out/rl2/.steps 2>/dev/null || echo 0)" -ge "$STEPS" ]; then
    touch "out/rl2/.done-$STEPS"
  fi
}

stage "out/rl2/.done-$STEPS" rl_until
N=$(ls -d out/rl2/checkpoint-* 2>/dev/null | sed 's/.*-//' | sort -n | tail -1)
N="${N:-0}"
REPORT=(base-eval sft-eval "rl-eval-$R1")
[ -e out/rollouts/r2-sft-eval.jsonl ] && REPORT+=(r2-sft-eval)
if [ "$N" -gt 0 ]; then
  stage "out/r2-merged-$N/config.json" "$PY" merge.py --base out/sft2-merged \
    --adapter "out/rl2/checkpoint-$N" --out "out/r2-merged-$N"
  roll "r2-eval-$N" "out/r2-merged-$N"
  rollx "r2-evalx-$N" "out/r2-merged-$N"
  REPORT+=("r2-eval-$N")
fi

"$PY" report.py --md out/REPORT2.md --rl-log out/rl2 --ref big-eval "${REPORT[@]}"
"$PY" report.py --md out/REPORT2-code.md --by variant --ref big-evalx r1-evalx "r2-evalx-$N"
echo "== report: out/REPORT2.md ($N round-2 RL steps, $(( ($(date +%s) - START) / 60 )) min)"
