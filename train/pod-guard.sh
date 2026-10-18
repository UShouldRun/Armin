#!/usr/bin/env bash
# Runs on the pod beside round2.sh, so a dead or hung run never keeps
# the GPU billing, even when the laptop (supervise.sh) is away.
#
#   setsid nohup ./pod-guard.sh > /workspace/armin/guard.log 2>&1 &
#
# Also installs /usr/local/bin/runpodctl, which holds "runpodctl stop"
# (round2.sh's AUTO_STOP) until supervise.sh has downloaded the results
# (/workspace/armin/.downloaded), for at most WAIT_MIN minutes.
set -u

W=/workspace/armin
WAIT_MIN="${WAIT_MIN:-60}"
HANG_MIN="${HANG_MIN:-60}"   # no log or checkpoint change, GPU idle
POD="${RUNPOD_POD_ID:-$(tr '\0' '\n' < /proc/1/environ | sed -n 's/^RUNPOD_POD_ID=//p')}"

cat > /usr/local/bin/runpodctl <<EOF
#!/bin/sh
# pod-guard.sh: wait for the download before stopping the pod
if [ "\$1" = stop ]; then
  # prep-local.sh still making the laptop's GGUFs (at most 3 h)
  i=0
  while [ -e $W/.prep ] && [ \$i -lt 360 ]; do
    sleep 30; i=\$(( i + 1 ))
  done
  i=0
  while [ ! -e $W/.downloaded ] && [ \$i -lt $(( WAIT_MIN * 2 )) ]; do
    sleep 30; i=\$(( i + 1 ))
  done
fi
exec /usr/bin/runpodctl "\$@"
EOF
chmod +x /usr/local/bin/runpodctl

log() { echo "$(date -u +%FT%H:%M) $*"; }

stop() {
  log "stopping: $1"
  pkill -9 -f "[r]ound2.sh"
  pkill -9 -f "[r]ollout.py|[r]l.py|[s]ft.py|[m]erge.py|VLLM::[E]ngineCore"
  /usr/local/bin/runpodctl stop pod "$POD"
  exit 0
}

log "guard up, pod $POD"
dead=0
while true; do
  sleep 300
  # .hold: restarting round2.sh by hand. "stopping pod": its own stop
  # is already waiting for the download.
  if [ -e $W/.hold ] || grep -q "^== stopping pod" $W/round2.log; then
    continue
  fi

  if ! pgrep -f "[r]ound2.sh" > /dev/null; then
    dead=$(( dead + 1 ))
    # 15 min: room to relaunch round2.sh by hand
    [ "$dead" -ge 3 ] && stop "round2.sh not running"
    continue
  fi

  dead=0
  (cd $W/train && python3 progress.py --rl out/rl2 --log $W/round2.log --out out/PROGRESS.md) \
    || log "progress.py failed"
  fresh=$(find $W/round2.log $W/train/out -newermt "-$HANG_MIN minutes" -print -quit 2> /dev/null)
  util=$(nvidia-smi --query-gpu=utilization.gpu --format=csv,noheader,nounits | head -1)
  if [ -z "$fresh" ] && [ "${util:-0}" -lt 5 ]; then
    stop "hung: nothing written for $HANG_MIN min, GPU at ${util}%"
  fi
done
