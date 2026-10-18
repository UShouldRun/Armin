#!/usr/bin/env bash
# Runs on the laptop, detached, while round2.sh runs on the pod:
#
#   setsid nohup ./supervise.sh > pod/supervise.log 2>&1 < /dev/null &
#
# Every 5 min: copies pod/PROGRESS.md (one row per 25 RL steps). Every
# 30 min: copies what a restart needs into pod/ (logs, reports,
# episodes, labels, warm-up sets, the round-1 and round-2 adapters and
# the newest RL checkpoint with its optimizer state). When round2.sh
# has ended (done, failed or killed by pod-guard.sh), downloads the
# best model, marks the pod downloaded so its stop goes through, and
# makes sure it stopped. pod/RESTART.md says how to go on.
set -u
cd "$(dirname "$0")"

R=/workspace/armin/train
L=pod
SSH=(ssh -o BatchMode=yes -o ConnectTimeout=20 runpod-armin)
SYNC=(rsync -rlt --partial -e "ssh -o BatchMode=yes -o ConnectTimeout=20")
mkdir -p $L/out/rollouts $L/data

log() { echo "$(date -u +%FT%H:%M) $*"; }

restart_info() {
  "${SYNC[@]}" runpod-armin:/workspace/armin/'*.log' $L/
  "${SYNC[@]}" --include='*.md' --include='rollouts/***' --include='.steps' \
    --exclude='*' runpod-armin:$R/out/ $L/out/
  "${SYNC[@]}" --include='*.labeled.jsonl' --include='sft_*.jsonl' --include='*x.jsonl' \
    --include='.round2' --exclude='*' runpod-armin:$R/data/ $L/data/
  # adapters: round 1's warm-up and RL, round 2's warm-up (small)
  for a in sft rl sft2; do
    "${SYNC[@]}" --include='adapter_*' --include='*.json' --include='*.jinja' --exclude='*' \
      runpod-armin:$R/out/$a/ $L/out/$a/ 2> /dev/null
  done
  # newest round-2 checkpoint, with optimizer state, for rl.py --resume
  n=$("${SSH[@]}" "ls -d $R/out/rl2/checkpoint-* 2>/dev/null | sed 's/.*-//' | sort -n | tail -1")
  if [ -n "$n" ]; then
    "${SYNC[@]}" --delete runpod-armin:$R/out/rl2/checkpoint-$n/ $L/out/rl2/checkpoint-$n/
    "${SYNC[@]}" runpod-armin:$R/out/rl2/.steps $L/out/rl2/ 2> /dev/null
    find $L/out/rl2 -maxdepth 1 -name 'checkpoint-*' ! -name "checkpoint-$n" -exec rm -rf {} +
  fi
  log "restart info synced (rl2 checkpoint ${n:-none})"
}

best_model() {
  # newest finished stage: round 2 RL merged, else round 2 warm-up,
  # else round 1. A round-2 RL checkpoint with no merge gets merged.
  "${SSH[@]}" "cd $R
    n=\$(ls -d out/rl2/checkpoint-* 2>/dev/null | sed 's/.*-//' | sort -n | tail -1)
    if [ -n \"\$n\" ] && [ ! -e out/r2-merged-\$n/config.json ] && [ -e out/sft2-merged/config.json ]; then
      .venv/bin/python merge.py --base out/sft2-merged --adapter out/rl2/checkpoint-\$n --out out/r2-merged-\$n >&2 || true
    fi
    for d in \$(ls -d out/r2-merged-* 2>/dev/null | sort -t- -k3 -n -r) out/sft2-merged \$(ls -d out/rl-merged-* | sort -t- -k3 -n -r); do
      [ -e \$d/config.json ] && { echo \$d; exit; }
    done"
}

finish() {
  log "round2.sh ended: $1"
  restart_info
  # the laptop test's GGUFs (prep-local.sh), at most 3 h
  for i in $(seq 1 36); do
    "${SSH[@]}" "[ -e /workspace/armin/.prep ]" || break
    sleep 300
  done
  log "downloading the GGUFs"
  until "${SYNC[@]}" --include='*.gguf' --exclude='*' runpod-armin:$R/out/gguf/ $L/gguf/; do
    log "GGUF download interrupted, retrying"; sleep 30
    "${SSH[@]}" true || break
  done
  m=$(best_model | tail -1)
  if [ -n "$m" ]; then
    log "downloading $m"
    until "${SYNC[@]}" runpod-armin:$R/$m/ $L/$m/; do
      log "download interrupted, retrying"; sleep 30
      "${SSH[@]}" true || { log "pod gone before the download finished"; break; }
    done
  fi

  cat > $L/RESTART.md <<EOF
# Round 2: what was saved ($(date -u +%F\ %H:%M) UTC)

Ended: $1
Best model: ${m:-none} (in pod/${m:-}). Newest RL checkpoint: $(ls -d $L/out/rl2/checkpoint-* 2>/dev/null | sed 's/.*-//' || echo none).
Progress: pod/out/PROGRESS.md. Logs: pod/*.log. Reports: pod/out/*.md.

To go on (the pod's /workspace volume also keeps all of it):
1. Start the pod again (Runpod console), \`./setup.sh\` if /root/venv is gone.
2. Copy back what the pod lacks: \`rsync -rlt pod/data/ pod/out/ runpod-armin:$R/...\`
3. \`BUDGET_HOURS=N AUTO_STOP=1 ./round2.sh\`: finished stages are
   skipped and RL resumes from its newest checkpoint (rl.py --resume).
EOF
  "${SSH[@]}" "touch /workspace/armin/.downloaded"
  log "marked downloaded; waiting for the pod to stop"
  for i in $(seq 1 20); do
    sleep 60
    "${SSH[@]}" true 2> /dev/null || { log "pod stopped"; exit 0; }
  done
  log "pod still up after 20 min; stopping it"
  "${SSH[@]}" 'runpodctl stop pod $(tr "\0" "\n" < /proc/1/environ | sed -n "s/^RUNPOD_POD_ID=//p")'
  exit 0
}

log "supervising"
i=0
down=0
while true; do
  if [ -e $L/.hold ]; then   # restarting round2.sh by hand
    sleep 60; continue
  fi

  # ended: round2.sh is gone, or it is in its exit trap, waiting
  # (in pod-guard's runpodctl) for this download
  st=$("${SSH[@]}" 'if [ -e /workspace/armin/.hold ]; then echo running;
    elif grep -q "^== stopping pod" /workspace/armin/round2.log \
    || ! pgrep -f "[r]ound2.sh" > /dev/null; then echo ended; else echo running; fi' 2> /dev/null)
  if [ -z "$st" ]; then
    down=$(( down + 1 ))
    log "pod unreachable ($down)"
    [ "$down" -ge 6 ] && { log "pod unreachable for 30 min; giving up"; exit 1; }
    sleep 300; continue
  fi

  down=0
  "${SYNC[@]}" runpod-armin:$R/out/PROGRESS.md $L/out/ 2> /dev/null
  [ "$st" = ended ] && finish "$("${SSH[@]}" 'tail -3 /workspace/armin/guard.log; tail -2 /workspace/armin/round2.log' | tr '\r' '\n' | tail -4)"
  [ $(( i % 6 )) = 0 ] && restart_info
  i=$(( i + 1 ))
  sleep 300
done
