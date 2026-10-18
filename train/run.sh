#!/usr/bin/env bash
# The whole pipeline, one stage after another. A stage whose output
# already exists is skipped, so after an interruption just run it
# again (RL resumes from its last checkpoint). SMOKE=1 runs every
# stage on a 0.6B model with tiny sizes, into out/smoke, to check
# the plumbing in minutes before paying for the real run.
#
#   ./run.sh                       # full run, STEPS RL steps
#   BUDGET_HOURS=13 ./run.sh       # as many RL steps as fit in 13 h
#   STEPS=400 ./run.sh             # later: continue RL to step 400
#   AUTO_STOP=1 ./run.sh           # stop this pod when done
#   SMOKE=1 ./run.sh               # plumbing check
#
# Ends with out/REPORT.md: how the trained model compares to the base.
set -euo pipefail
cd "$(dirname "$0")"

export ARMIN_BASE="${ARMIN_BASE:-Qwen/Qwen3-8B}"
# FlashInfer's sampler JIT-compiles on first use (needs ninja and an
# nvcc matching torch's CUDA); vLLM's own sampler needs neither.
export VLLM_USE_FLASHINFER_SAMPLER=0
PY="${PY:-.venv/bin/python}"

P=""              # prefix of every name this run writes
O=out             # model directory
LIMIT_SFT="${LIMIT_SFT:-1200}"  # sft tasks sampled for self-distillation (0 = all)
LIMIT_RL="${LIMIT_RL:-800}"     # rl tasks labeled; RL uses 4 per step
LIMIT_EVAL="${LIMIT_EVAL:-400}" # eval tasks per stage (0 = all 633)
EPOCHS=2
START=$(date +%s)

# Stop the pod however the script ends, so a failure can't keep the
# GPU billing. The network volume keeps data, models and checkpoints.
stop_pod() {
  if [ "${AUTO_STOP:-0}" = 1 ] && [ -n "${RUNPOD_POD_ID:-}" ]; then
    echo "== stopping pod $RUNPOD_POD_ID"
    runpodctl stop pod "$RUNPOD_POD_ID"
  fi
}

trap stop_pod EXIT
RESERVE=4500      # seconds kept after RL for merge, eval and report
if [ "${SMOKE:-0}" = 1 ]; then
  export ARMIN_BASE="Qwen/Qwen3-0.6B"
  P="smoke-" O=out/smoke
  LIMIT_SFT=24 LIMIT_RL=16 LIMIT_EVAL=24 STEPS="${STEPS:-3}" EPOCHS=1
fi

# Rerun with a higher STEPS to continue RL from its last checkpoint.
# With BUDGET_HOURS, STEPS is only a cap: RL stops at the deadline.
DEADLINE=0
if [ -n "${BUDGET_HOURS:-}" ]; then
  DEADLINE=$(python3 -c "print(int($START + $BUDGET_HOURS * 3600 - $RESERVE))")
  STEPS="${STEPS:-2000}"
fi

STEPS="${STEPS:-250}"

stage() {  # stage <output that marks it done> <command...>
  local done="$1"; shift
  if [ -e "$done" ]; then
    echo "-- skip, $done exists"
    return
  fi

  echo "== $*"
  local rc=0
  "$@" || rc=$?
  # A crashed vLLM leaves its engine process holding the GPU.
  pkill -9 -f "VLLM::EngineCore" 2>/dev/null || true
  return "$rc"
}

roll() {  # roll <name> <model> <split> <mode> <n> <limit>
  stage "out/rollouts/$P$1.jsonl" "$PY" rollout.py --model "$2" \
    --split "$3" --mode "$4" --n "$5" --limit "$6" --out "$P$1"
}

stage data/system.txt "$PY" prepare.py
"$PY" check.py --split eval --per-kind 10

roll base-eval   "$ARMIN_BASE" eval free    1 "$LIMIT_EVAL"
roll base-rl-nt  "$ARMIN_BASE" rl   nothink 2 "$LIMIT_RL"
roll base-sft-nt "$ARMIN_BASE" sft  nothink 2 "$LIMIT_SFT"
roll base-sft-fr "$ARMIN_BASE" sft  free    2 "$LIMIT_SFT"

stage "data/${P}rl.labeled.jsonl" "$PY" label.py --split rl \
  --rollouts "${P}base-rl-nt" --out "${P}rl.labeled"
stage "data/${P}sft_train.jsonl" "$PY" build_sft.py \
  --nothink "${P}base-sft-nt" --free "${P}base-sft-fr" --out "${P}sft_train"

stage "$O/sft/adapter_config.json" "$PY" sft.py --model "$ARMIN_BASE" \
  --data "${P}sft_train" --out "$O/sft" --epochs "$EPOCHS"
stage "$O/sft-merged/config.json" "$PY" merge.py --base "$ARMIN_BASE" \
  --adapter "$O/sft" --out "$O/sft-merged"
roll sft-eval "$O/sft-merged" eval free 1 "$LIMIT_EVAL"

# RL resumes from its last checkpoint, so a higher STEPS (or a new
# budget) continues it. The result is named by the steps it reached.
rl_until() {
  if [ "$DEADLINE" != 0 ] && [ $(( DEADLINE - $(date +%s) )) -lt 900 ]; then
    echo "-- no time left for RL before the deadline"
    return
  fi

  # A crash still leaves checkpoints, which the merge below uses.
  "$PY" rl.py --model "$O/sft-merged" --tasks "${P}rl.labeled" \
    --out "$O/rl" --steps "$STEPS" --deadline "$DEADLINE" --resume \
    || echo "-- rl.py exited with $?; using its last checkpoint"
  if [ "$(cat "$O/rl/.steps" 2>/dev/null || echo 0)" -ge "$STEPS" ]; then
    touch "$O/rl/.done-$STEPS"
  fi
}

stage "$O/rl/.done-$STEPS" rl_until
# The last step is always a checkpoint (deadline stop or save_steps).
N=$(ls -d "$O"/rl/checkpoint-* 2>/dev/null | sed 's/.*-//' | sort -n | tail -1)
N="${N:-0}"
REPORT=("${P}base-eval" "${P}sft-eval")
if [ "$N" -gt 0 ]; then
  stage "$O/rl-merged-$N/config.json" "$PY" merge.py --base "$O/sft-merged" \
    --adapter "$O/rl/checkpoint-$N" --out "$O/rl-merged-$N"
  roll "rl-eval-$N" "$O/rl-merged-$N" eval free 1 "$LIMIT_EVAL"
  REPORT+=("${P}rl-eval-$N")
fi

"$PY" report.py --md "$O/REPORT.md" --rl-log "$O/rl" "${REPORT[@]}"
echo "== report: $O/REPORT.md ($N RL steps, $(( ($(date +%s) - START) / 60 )) min)"


