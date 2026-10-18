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
#                     [--update-skills] [--install-deps | --no-deps]
#                     [--library-sync | --no-library-sync]
#                     [--library-model NAME]
#
#   --prefix DIR      install the binary here (default: ~/.local/bin)
#   --no-config       skip writing a starter config.conf
#   --no-skills       skip installing the bundled skills
#   --update-skills   overwrite installed copies of the bundled skills
#   --install-deps    install missing commands without asking
#   --no-deps         only report missing commands, never offer to
#                     install them

set -eu

prefix="${HOME}/.local/bin"
write_config=1
install_skills=1
update_skills=0
deps=ask
library_sync=ask
library_model=""

usage() {
  echo "Usage: ./install.sh [--prefix DIR] [--no-config] [--no-skills] [--update-skills]"
  echo
  echo "  --prefix DIR      install the binary here (default: ~/.local/bin)"
  echo "  --no-config       skip writing a starter config.conf"
  echo "  --no-skills       skip installing the bundled skills"
  echo "  --update-skills   overwrite installed copies of the bundled skills"
  echo "  --install-deps    install missing commands without asking"
  echo "  --no-deps         only report missing commands, never offer to install them"
  echo "  --library-sync    catalog new library books without asking (--no-library-sync: never)"
  echo "  --library-model M the model that catalogs them (default: config.conf's model)"
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
    --install-deps)
      deps=yes
      shift
      ;;
    --no-deps)
      deps=no
      shift
      ;;
    --library-sync)
      library_sync=yes
      shift
      ;;
    --no-library-sync)
      library_sync=no
      shift
      ;;
    --library-model)
      [ $# -ge 2 ] || { echo "install.sh: --library-model needs a value" >&2; exit 1; }
      library_model="$2"
      shift 2
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
# Copy then rename: renaming over a running binary is fine, while
# writing into it fails with "Text file busy".
cp armin "$prefix/armin.new"
mv -f "$prefix/armin.new" "$prefix/armin"
chmod +x "$prefix/armin"
ok "installed to $prefix/armin"

case ":$PATH:" in
  *":$prefix:"*) ;;
  *)
    warn "$prefix isn't on your PATH — add this to your shell rc:"
    echo "      export PATH=\"$prefix:\$PATH\""
    ;;
esac

# --- Neovim plugin ------------------------------------------------------
# A symlink, not a copy: a copy goes stale on the next git pull while
# the binary it talks to moves on.

nvim_lua="${XDG_CONFIG_HOME:-$HOME/.config}/nvim/lua"
if [ -d "$nvim_lua" ]; then
  plugin="$(pwd)/editors/nvim/armin.lua"
  if [ -L "$nvim_lua/armin.lua" ] || [ ! -e "$nvim_lua/armin.lua" ]; then
    ln -sfn "$plugin" "$nvim_lua/armin.lua"
    ok "nvim plugin linked: $nvim_lua/armin.lua"
  else
    mv -f "$nvim_lua/armin.lua" "$nvim_lua/armin.lua.bak"
    ln -s "$plugin" "$nvim_lua/armin.lua"
    warn "replaced an old copy of the nvim plugin with a link (kept armin.lua.bak)"
  fi
fi

echo
echo "checking dependencies:"

# --- Commands armin runs -----------------------------------------------
# Each group lists commands and what armin can't do without them.
# Missing ones are offered for install, one question per group, with
# the exact command shown; declining just reports what's lost.

pm=""
for c in apt-get dnf pacman brew; do
  if command -v "$c" >/dev/null 2>&1; then
    pm="$c"
    break
  fi
done

sudo="sudo "
if [ "$(id -u)" = 0 ] || [ "$pm" = brew ]; then
  sudo=""
fi

case "$pm" in
  apt-get) install_cmd="${sudo}apt-get update -qq && ${sudo}apt-get install -y" ;;
  dnf)     install_cmd="${sudo}dnf install -y" ;;
  pacman)  install_cmd="${sudo}pacman -S --needed --noconfirm" ;;
  brew)    install_cmd="brew install" ;;
  *)       install_cmd="" ;;
esac

# pkg BIN prints the package that provides BIN under $pm.
pkg() {
  case "$pm:$1" in
    *:rg)                          echo ripgrep ;;
    apt-get:pdf*|dnf:pdf*)         echo poppler-utils ;;
    *:pdf*)                        echo poppler ;;
    apt-get:g++)                   echo g++ ;;
    dnf:g++)                       echo gcc-c++ ;;
    *:g++)                         echo gcc ;;
    apt-get:scp)                   echo openssh-client ;;
    dnf:scp)                       echo openssh-clients ;;
    *:scp)                         echo openssh ;;
    apt-get:mpi*)                  echo "openmpi-bin libopenmpi-dev" ;;
    *:mpi*)                        echo openmpi ;;
    apt-get:which)                 echo debianutils ;;
    *:find)                        echo findutils ;;
    *:diff)                        echo diffutils ;;
    *:cat|*:ls|*:head|*:tail|*:wc|*:sort|*:uniq|*:cut|*:tr|*:stat|*:du|*:df|*:nl|*:realpath|*:sha256sum|*:md5sum)
                                   echo coreutils ;;
    *)                             echo "$1" ;;
  esac
}

# group TITLE "BIN,BIN=what armin loses" ...
group() {
  title="$1"
  shift
  missing=""
  pkgs=""
  lost=""
  for entry in "$@"; do
    gone=""
    for b in $(echo "${entry%%=*}" | tr ',' ' '); do
      command -v "$b" >/dev/null 2>&1 || gone="$gone $b"
    done

    [ -n "$gone" ] || continue
    missing="$missing$gone"
    lost="$lost
        -$gone: ${entry#*=}"
    for b in $gone; do
      p=$(pkg "$b")
      case " $pkgs " in
        *" $p "*) ;;
        *) pkgs="$pkgs $p" ;;
      esac
    done
  done

  if [ -z "$missing" ]; then
    ok "$title: all found"
    return
  fi

  warn "$title: missing$missing"
  offer "$title" "$pkgs" "$lost"
}

# offer TITLE PACKAGES LOST asks (or, with --install-deps, doesn't)
# and installs; otherwise says what armin will lack.
offer() {
  if [ -z "$install_cmd" ]; then
    echo "      no apt, dnf, pacman or brew found: install$2 yourself"
  elif [ "$deps" != no ]; then
    answer=n
    if [ "$deps" = yes ]; then
      answer=y
    elif [ -t 0 ]; then
      printf '      install them now with: %s%s ? [y/N] ' "$install_cmd" "$2"
      read -r answer || answer=n
    fi

    case "$answer" in
      y|Y|yes)
        if sh -c "$install_cmd$2"; then
          ok "$1: installed$2"
          return
        fi

        warn "$1: installing failed"
        ;;
    esac
  fi

  echo "      without them, armin can't do:$3"
}

group "armin's own tools" \
  "rg=search_code (finding code in the project)" \
  "git=git history, diffs, blame and working-tree tools" \
  "pdftotext,pdfinfo=read_pdf (reading PDFs)" \
  "tree=directory trees through run_command" \
  "file=telling file types apart through run_command" \
  "grep,find,sed,diff,which=run_command's search and inspection commands" \
  "cat,ls,head,tail,wc,sort,uniq,cut,tr,stat,du,df,nl,realpath,sha256sum,md5sum=run_command's basic commands"

group "C and C++ builds (run_program)" \
  "make=make builds, and build_check / run_tests for Makefile projects" \
  "gcc,g++=compiling C and C++" \
  "cmake=CMake projects" \
  "gdb,valgrind=debugging and memory checks"

group "copying files to other machines (run_program, always asked)" \
  "rsync=rsync transfers" \
  "scp=scp transfers"

group "MPI (run_program)" \
  "mpicc,mpirun=compiling and running MPI programs"

# Toolchains run_program can use: reported, never installed here —
# which ones you want is your call.
found=""
absent=""
for b in go cargo rustc python3 pytest node npm java javac mvn gradle dotnet deno bun zig typst pdflatex; do
  if command -v "$b" >/dev/null 2>&1; then
    found="$found $b"
  else
    absent="$absent $b"
  fi
done

ok "other toolchains run_program can use:${found:- none}"
if [ -n "$absent" ]; then
  echo "      not installed (run_program can't use them until you install them):$absent"
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

# --- Reference library -------------------------------------------------------
# library = <folder> in config.conf: books and docs the agent searches
# with library_search. A model catalogs each new book into the topic
# table (where each subject is covered); that runs here when books
# are waiting, since it can take a while.

echo
echo "reference library:"
library_dir=$(sed -n 's/^[[:space:]]*library[[:space:]]*=[[:space:]]*//p' "$config_path" 2> /dev/null | tail -1)
library_dir=${library_dir/#\~/$HOME}

if [ -z "$library_dir" ]; then
  warn "none configured — add library = <folder> to $config_path"
  warn "docs/library-fetch.sh downloads a starter set of free books and docs into ~/Documents/Software"
elif [ ! -d "$library_dir" ]; then
  warn "library = $library_dir, but that folder doesn't exist"
else
  ok "library at $library_dir"
  model_flag=""
  [ -n "$library_model" ] && model_flag="-model $library_model"
  sync=$library_sync
  if [ "$sync" = ask ]; then
    if [ -t 0 ]; then
      printf '  index it and have a model catalog new books now? It can take a while. [y/N] '
      read -r answer
      case "$answer" in y|Y|yes) sync=yes ;; *) sync=no ;; esac
    else
      sync=no
    fi
  fi

  if [ "$sync" = yes ]; then
    "$prefix/armin" -library-sync -new -no-save $model_flag || warn "library sync failed — run armin -library-sync later"
  else
    warn "later: armin -library-sync (or /library catalog inside armin)"
  fi
fi

echo
echo "done. next steps:"
echo "  1. have a llama-server running at http://localhost:8080 (or edit"
echo "     $config_path to point elsewhere, or add a managed models.<name> entry)"
echo "  2. armin -root /path/to/your/project"
