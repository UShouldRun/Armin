#!/bin/sh
# install.sh builds armin and installs it to a directory on PATH,
# then reports on the external tools armin needs or can use — every
# check prints what it found and, for anything missing, exactly what
# to do about it, mirroring the "degrade gracefully, never silently"
# philosophy the tools themselves already follow (see README.md).
# Safe to re-run any time (e.g. after `git pull`): it only ever
# rebuilds/reinstalls the binary and never touches an existing
# config.conf.
#
# Usage: ./install.sh [--prefix DIR] [--no-config] [--no-skills]
#                     [--update-skills]
#
#   --prefix DIR      install the binary here (default: ~/.local/bin)
#   --no-config       skip writing a starter config.conf
#   --no-skills       skip installing the bundled skills
#   --update-skills   overwrite installed copies of the bundled skills

set -eu

prefix="${HOME}/.local/bin"
write_config=1
install_skills=1
update_skills=0

usage() {
  echo "Usage: ./install.sh [--prefix DIR] [--no-config] [--no-skills] [--update-skills]"
  echo
  echo "  --prefix DIR      install the binary here (default: ~/.local/bin)"
  echo "  --no-config       skip writing a starter config.conf"
  echo "  --no-skills       skip installing the bundled skills"
  echo "  --update-skills   overwrite installed copies of the bundled skills"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --prefix)
      [ $# -ge 2 ] || { echo "install.sh: --prefix needs a value" >&2; exit 1; }
      prefix="$2"
      shift 2
      ;;
    --prefix=*)
      prefix="${1#--prefix=}"
      shift
      ;;
    --no-config)
      write_config=0
      shift
      ;;
    --no-skills)
      install_skills=0
      shift
      ;;
    --update-skills)
      update_skills=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "install.sh: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  c_ok=$(printf '\033[32m'); c_warn=$(printf '\033[33m')
  c_err=$(printf '\033[31m'); c_reset=$(printf '\033[0m')
else
  c_ok=""; c_warn=""; c_err=""; c_reset=""
fi
ok()   { printf '  %s✓%s %s\n' "$c_ok" "$c_reset" "$1"; }
warn() { printf '  %s!%s %s\n' "$c_warn" "$c_reset" "$1"; }
err()  { printf '  %s✗%s %s\n' "$c_err" "$c_reset" "$1" >&2; }

echo "armin install"
echo

# --- Go toolchain ---------------------------------------------------
# go build itself enforces go.mod's minimum version — no need to
# parse/compare semver here, just confirm a toolchain exists at all.

if ! command -v go >/dev/null 2>&1; then
  err "go not found on PATH — install Go 1.23+ from https://go.dev/dl/ and re-run this script"
  exit 1
fi
ok "$(go version)"

# --- Build ------------------------------------------------------------

echo "building ./cmd/armin ..."
go build -o armin ./cmd/armin
ok "built ./armin"

# --- Install ------------------------------------------------------------

mkdir -p "$prefix"
cp armin "$prefix/armin"
chmod +x "$prefix/armin"
ok "installed to $prefix/armin"

case ":$PATH:" in
  *":$prefix:"*) ;;
  *)
    warn "$prefix isn't on your PATH — add this to your shell rc:"
    echo "      export PATH=\"$prefix:\$PATH\""
    ;;
esac

echo
echo "checking dependencies:"

# --- Required: ripgrep ------------------------------------------------

if command -v rg >/dev/null 2>&1; then
  ok "ripgrep found ($(command -v rg))"
else
  warn "ripgrep (rg) not found — search_code won't work until it's installed"
  echo "      https://github.com/BurntSushi/ripgrep#installation"
fi

# --- Required (running somewhere): llama-server ------------------------

if command -v llama-server >/dev/null 2>&1; then
  ok "llama-server found ($(command -v llama-server)) — armin can spawn/manage it itself via a models.<name> entry"
else
  warn "llama-server not found on PATH"
  echo "      build/install it from https://github.com/ggml-org/llama.cpp so armin"
  echo "      can spawn it itself, or point -llama-url / config.conf's llama_url at"
  echo "      one you already run elsewhere (see README.md's \"Model lifecycle\")."
fi

# --- Optional: docker (web_search) --------------------------------------

if command -v docker >/dev/null 2>&1; then
  ok "docker found — web_search will work"
else
  warn "docker not found — web_search will report a clean \"unavailable\" error (everything else still works)"
fi

# --- Optional: universal-ctags (-lsp symbol features) -------------------

if command -v universal-ctags >/dev/null 2>&1; then
  ok "universal-ctags found — -lsp definition/workspace-symbol/hover grounding enabled"
elif [ -x "${HOME}/.local/bin/universal-ctags" ]; then
  ok "universal-ctags found at ~/.local/bin/universal-ctags"
else
  warn "universal-ctags not found — -lsp's go-to-definition/workspace-symbol degrade gracefully without it"
  echo "      (the plain \"ctags\" most distros ship is Emacs's, a different, incompatible tool — see README.md)"
fi

# --- Starter config -------------------------------------------------------

config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/armin"
config_path="$config_dir/config.conf"
env_path="$config_dir/.env"

if [ "$write_config" -eq 1 ]; then
  mkdir -p "$config_dir"

  if [ -e "$config_path" ]; then
    ok "config.conf already exists at $config_path — left untouched"
  else
    cat > "$config_path" <<'EOF'
# key = value, "#" comments, nothing fancier — see README.md's
# "Config file" and "Model lifecycle" sections for the full key list.
model     = chat
llama_url = http://localhost:8080
EOF
    ok "wrote a starter config to $config_path"
  fi

  # .env holds API keys for any models.<name> pointed at an
  # authenticated remote endpoint (see README.md's "Authenticated /
  # remote models" section) — armin refuses to read one that's
  # readable by anyone but you, so this is created (and any existing
  # one re-tightened) at mode 600 from the start.
  if [ -e "$env_path" ]; then
    chmod 600 "$env_path"
    ok ".env already exists at $env_path — content left untouched, permissions set to 600"
  else
    cat > "$env_path" <<'EOF'
# key = value, "#" comments, nothing fancier. Only needed for a
# models.<name> pointed at an authenticated remote endpoint (Claude's
# OpenAI-compatible API, say) — a local llama-server needs none of
# this. Must stay readable only by you — armin refuses to load it
# otherwise — which is why this file is created at mode 600.
#
# ANTHROPIC_API_KEY = sk-ant-...
EOF
    chmod 600 "$env_path"
    ok "wrote an empty .env template to $env_path (mode 600)"
  fi
fi

# --- Bundled skills ---------------------------------------------------------
# Copied to the user skill directory, where armin loads them for every
# project. An installed skill is never overwritten (it may have been
# edited) unless --update-skills is given.

skills_dir="$config_dir/skills"

if [ "$install_skills" -eq 1 ] && [ -d skills ]; then
  mkdir -p "$skills_dir"

  for src in skills/*/; do
    name=$(basename "$src")
    dst="$skills_dir/$name"

    if [ -e "$dst" ] && [ "$update_skills" -eq 0 ]; then
      ok "skill $name already installed — left untouched (--update-skills to replace)"
      continue
    fi

    rm -rf "$dst"
    cp -R "$src" "$dst"
    ok "installed skill $name to $dst"
  done
fi

echo
echo "done. next steps:"
echo "  1. have a llama-server running at http://localhost:8080 (or edit"
echo "     $config_path to point elsewhere, or add a managed models.<name> entry)"
echo "  2. armin -root /path/to/your/project"
