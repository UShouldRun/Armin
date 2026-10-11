---
name: linux
description: Diagnose and operate a Linux (Ubuntu) machine — processes, disk, memory, GPU, services, logs, networking, permissions, packages — reading state first and asking before anything destructive or system-wide. Load for system administration or "why is my machine…" questions.
---
## Read state first
| Question | Command |
|---|---|
| what's using CPU / memory | `top -o %CPU`, `ps aux --sort=-%mem \| head` |
| memory | `free -h` |
| disk space / what's big | `df -h`, `du -sh * \| sort -h \| tail`, `ncdu` |
| GPU | `nvidia-smi`, `nvidia-smi --query-gpu=memory.used,utilization.gpu --format=csv` |
| who listens on a port | `ss -ltnp`, `lsof -i :8080` |
| a process's files / limits | `ls -l /proc/<pid>/fd`, `cat /proc/<pid>/limits` |
| logs | `journalctl -u <service> -n 100 --no-pager`, `journalctl -b -p err`, `dmesg -T \| tail` |
| services | `systemctl status <name>`, `systemctl --user status <name>` |
| hardware / kernel | `lscpu`, `lsblk`, `lspci`, `uname -a` |

## Processes
- Stop politely first: `kill <pid>` (SIGTERM), wait, then
  `kill -9` only if it ignores that. `pkill -f pattern` matches full
  command lines — check with `pgrep -af pattern` first.
- Long jobs: run in the background with output to a log
  (`cmd > log 2>&1 &`) or in `tmux`, and check the log.

## Disk
- On a nearly full root partition, look for caches first:
  `~/.cache`, old snap revisions (`snap list --all`), `docker system
  df`, `journalctl --disk-usage`. Large data (models, datasets) can
  live on another partition — check `lsblk`/`df -h` for one with room.
- Never delete outside the user's own files without asking; show
  what would be removed and how much it frees.

## Permissions
- `ls -l`, `stat`, `id`. Fix with the narrowest change:
  `chmod 600 secret`, `chmod +x script`, `chown` on the user's own
  files. Avoid `chmod 777` and recursive changes on system paths.
- Secrets (.env, keys) should be mode 600.

## Packages and system changes
- Ubuntu: `apt list --installed`, `apt policy <pkg>`;
  install with `sudo apt install <pkg>` — **ask first**; sudo,
  package changes, services, firewall and kernel settings are
  system-wide.
- Prefer user-level installs (`~/.local/bin`, `pipx`, `go install`)
  when they work.

## Networking
`ip -br addr`, `ip route`, `resolvectl status`, `ping -c 3 host`,
`curl -sv URL`, `ss -tunap`. For a service that won't connect: is it
listening (`ss -ltnp`), on which address (127.0.0.1 vs 0.0.0.0), and
is a firewall in the way (`sudo ufw status`)?

## Never without asking
`rm -rf` on anything not clearly the user's scratch, `dd`, `mkfs`,
partitioning, editing `/etc`, `sudo` anything, stopping system
services, `kill -9` on processes you didn't start, changing
permissions recursively. Say what it does and what could be lost.
