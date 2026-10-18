"""Runs Python and pytest for the tool environment and the grader:
no network (when user namespaces exist), capped memory, CPU time
and file size, and the whole process group killed on timeout.
Standard library only; pytest must be installed to run tests."""

import os
import signal
import subprocess
import sys

TIMEOUT    = 10
MEM_BYTES  = 2 << 30
FILE_BYTES = 16 << 20
OUT_TAIL   = 4000

# Limits are set in the child itself, so callers need no
# preexec_fn (unsafe in threaded processes like the RL trainer).
_LAUNCH = (
  "import os, resource, sys\n"
  "resource.setrlimit(resource.RLIMIT_AS, ({m}, {m}))\n"
  "resource.setrlimit(resource.RLIMIT_CPU, ({c}, {c}))\n"
  "resource.setrlimit(resource.RLIMIT_FSIZE, ({f}, {f}))\n"
  "os.execv(sys.executable, [sys.executable] + sys.argv[1:])\n"
).format(m = MEM_BYTES, c = TIMEOUT, f = FILE_BYTES)


def _can_unshare():
  try:
    r = subprocess.run(
      ["unshare", "-rn", "true"],
      capture_output = True,
      timeout        = 5,
    )

  except (OSError, subprocess.TimeoutExpired):
    return False

  return r.returncode == 0


UNSHARE = _can_unshare()
if not UNSHARE:
  print("sandbox: no user namespaces, code runs with network", file = sys.stderr)


def run_python(args, cwd, stdin = None):
  """Runs `python3 <args>` in cwd. Returns (exit code, output tail);
  exit code -1 means it timed out."""
  cmd = [sys.executable, "-c", _LAUNCH] + list(args)
  if UNSHARE:
    cmd = ["unshare", "-rn"] + cmd

  env = {
    "PATH":                    os.environ.get("PATH", ""),
    "HOME":                    cwd,
    "PYTHONDONTWRITEBYTECODE": "1",
    "PYTHONHASHSEED":          "0",
  }
  feed = subprocess.DEVNULL
  if stdin is not None:
    feed = open(stdin, "rb")

  try:
    proc = subprocess.Popen(
      cmd,
      cwd               = cwd,
      env               = env,
      stdin             = feed,
      stdout            = subprocess.PIPE,
      stderr            = subprocess.STDOUT,
      start_new_session = True,
    )
    try:
      out, _ = proc.communicate(timeout = TIMEOUT + 5)

    except subprocess.TimeoutExpired:
      os.killpg(proc.pid, signal.SIGKILL)
      proc.communicate()
      return -1, f"timed out after {TIMEOUT}s"

  finally:
    if stdin is not None:
      feed.close()

  return proc.returncode, out.decode("utf-8", "replace")[-OUT_TAIL:]


def run_pytest(cwd, extra = ()):
  return run_python(
    ["-m", "pytest", "-q", "-p", "no:cacheprovider"] + list(extra),
    cwd,
  )
