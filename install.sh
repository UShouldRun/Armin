#!/bin/sh
# install.sh builds armin and installs it to a directory on PATH,
# then reports on the external tools armin needs or can use — every
# check prints what it found and, for anything missing, exactly what
# to do about it, mirroring the "degrade gracefully, never silently"
# philosophy the tools themselves already follow (see README.md).
# Safe to re-run any time (e.g. after `git pull`): it rebuilds and
# reinstalls the binary, installs only what's missing, and never
# touches an existing config.conf.
#
# What armin needs to run is installed when missing, after asking
# (yes by default): Go to build it, llama.cpp's llama-server to run
# local models, ripgrep to search code. A version control system it
# supports (git) is checked for and recommended, never installed.
# Optional tools are offered, and declining says what armin loses.
#
# Usage: ./install.sh [--prefix DIR] [--no-config] [--no-skills]
#                     [--update-skills] [--install-deps | --no-deps]
#                     [--library-sync | --no-library-sync]
#                     [--library-model NAME] [--ram TYPE-SPEED]
#
#   --prefix DIR      install the binary here (default: ~/.local/bin)
#   --no-config       skip writing a starter config.conf
#   --no-skills       skip installing the bundled skills
#   --update-skills   overwrite installed copies of the bundled skills
#   --install-deps    install what's missing without asking
#   --no-deps         only report what's missing, never install it
#   --ram TYPE-SPEED  the system RAM, e.g. ddr5-5600, for armin
#                     -install's estimates (default: read it with
#                     dmidecode, which needs sudo, or ask)

set -eu

prefix="${HOME}/.local/bin"
write_config=1
install_skills=1
update_skills=0
deps=ask
library_sync=ask
library_model=""
ram=""

usage() {
  echo "Usage: ./install.sh [--prefix DIR] [--no-config] [--no-skills] [--update-skills]"
  echo
  echo "  --prefix DIR      install the binary here (default: ~/.local/bin)"
  echo "  --no-config       skip writing a starter config.conf"
  echo "  --no-skills       skip installing the bundled skills"
  echo "  --update-skills   overwrite installed copies of the bundled skills"
  echo "  --install-deps    install what's missing without asking"
  echo "  --no-deps         only report what's missing, never install it"
  echo "  --library-sync    catalog new library books without asking (--no-library-sync: never)"
  echo "  --library-model M the model that catalogs them (default: config.conf's model)"
  echo "  --ram TYPE-SPEED  the system RAM, e.g. ddr5-5600 or ddr4-3200 1ch (default: detect or ask)"
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
    --ram)
      [ $# -ge 2 ] || { echo "install.sh: --ram needs a value" >&2; exit 1; }
      ram="$2"
      shift 2
      ;;
    --ram=*)
      ram="${1#--ram=}"
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

config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/armin"
config_path="$config_dir/config.conf"

# want QUESTION asks yes-or-no for something armin needs: yes unless
# the answer is no; --install-deps says yes, --no-deps says no, and a
# script with no terminal gets yes.
want() {
  case "$deps" in
    yes) return 0 ;;
    no)  return 1 ;;
  esac

  [ -t 0 ] || return 0
  printf '      %s [Y/n] ' "$1"
  read -r answer || answer=y
  case "$answer" in
    n|N|no) return 1 ;;
  esac

  return 0
}

# fetch URL writes URL to stdout, with curl or wget.
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "$1"
  else
    err "neither curl nor wget is installed: install one and re-run"
    exit 1
  fi
}

# --- Go toolchain ---------------------------------------------------
# Go 1.21 and later fetch the exact toolchain go.mod asks for by
# themselves, so anything from 1.21 builds armin. Older or missing,
# the official release goes into ~/.local/go: no sudo, no distro
# package that lags behind.

go_usable() {
  command -v go >/dev/null 2>&1 &&
    go version | grep -Eq 'go1\.(2[1-9]|[3-9][0-9])|go[2-9]'
}

go_root="$HOME/.local/go"
if ! go_usable && [ -x "$go_root/bin/go" ]; then
  PATH="$go_root/bin:$PATH"
fi

if ! go_usable; then
  warn "Go 1.21 or later is needed to build armin, and isn't installed"
  if ! want "install the latest Go into $go_root?"; then
    err "install Go from https://go.dev/dl/ and re-run this script"
    exit 1
  fi

  case "$(uname -s)" in
    Linux)  go_os=linux ;;
    Darwin) go_os=darwin ;;
    *)      err "no Go release for $(uname -s): install it by hand"; exit 1 ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64)  go_arch=amd64 ;;
    aarch64|arm64) go_arch=arm64 ;;
    *)             err "no Go release for $(uname -m): install it by hand"; exit 1 ;;
  esac

  go_version=$(fetch "https://go.dev/VERSION?m=text" | head -n 1)
  rm -rf "$go_root"
  mkdir -p "$HOME/.local"
  fetch "https://go.dev/dl/$go_version.$go_os-$go_arch.tar.gz" |
    tar -xz -C "$HOME/.local"
  PATH="$go_root/bin:$PATH"
  ok "installed $go_version into $go_root"
  warn "add Go to your PATH in your shell rc:"
  echo "      export PATH=\"$go_root/bin:\$PATH\""
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

# --- Manual page ----------------------------------------------------------
# man finds ~/.local/share/man on its own when ~/.local/bin is on PATH.

man_dir="${XDG_DATA_HOME:-$HOME/.local/share}/man/man1"
mkdir -p "$man_dir"
cp docs/armin.1 "$man_dir/armin.1"
ok "manual page installed: man armin"

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

# --- What armin can't run without ---------------------------------------

if command -v rg >/dev/null 2>&1; then
  ok "ripgrep found"
elif [ -n "$install_cmd" ] &&
  want "armin searches code with ripgrep, which isn't installed: install with $install_cmd $(pkg rg)?"; then
  if sh -c "$install_cmd $(pkg rg)"; then
    ok "installed ripgrep"
  else
    err "installing ripgrep failed: armin can't search code without it"
  fi
else
  err "armin needs ripgrep to search code: install it and re-run"
fi

# --- Version control -------------------------------------------------------
# armin reads history, diffs and blame through the version control
# systems it has tools for; one of them must be on the system. It's
# never installed here: which one a project uses is the user's call.

vcs_supported="git"
vcs=""
for b in $vcs_supported; do
  if command -v "$b" >/dev/null 2>&1; then
    vcs="$vcs $b"
  fi
done

if [ -n "$vcs" ]; then
  ok "version control:$vcs"
else
  warn "no version control system armin supports ($vcs_supported) is installed"
  echo "      without one, armin can't read history, diffs or blame (git_log,"
  echo "      git_diff, git_show, git_blame, working_tree_diff). Git is the"
  echo "      one to install: ${install_cmd:+$install_cmd }$(pkg git)"
fi

group "armin's own tools" \
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

# --- llama.cpp: llama-server, for local models ---------------------------
# Found on PATH, or named by a models.<name>.binary in config.conf, it
# is left alone. Otherwise it is built from llama.cpp's own repository
# into ~/.local/share/armin/llama.cpp, statically linked so the copy
# put next to armin stands alone: with CUDA when the CUDA toolkit is
# there, Metal on a Mac, the CPU otherwise.

llama=""
if command -v llama-server >/dev/null 2>&1; then
  llama=$(command -v llama-server)
elif [ -f "$config_path" ]; then
  for b in $(sed -n 's/^ *models\.[^.]*\.binary *= *//p' "$config_path"); do
    if [ -x "$b" ]; then
      llama="$b"
      break
    fi
  done
fi

if [ -n "$llama" ]; then
  ok "llama-server found ($llama)"
elif want "llama-server (llama.cpp) runs armin's local models and isn't installed: build it now?"; then
  build_need=""
  for b in cmake make; do
    command -v "$b" >/dev/null 2>&1 || build_need="$build_need $(pkg "$b")"
  done

  if ! command -v c++ >/dev/null 2>&1; then
    case "$pm" in
      apt-get) build_need="$build_need build-essential" ;;
      *)       build_need="$build_need $(pkg g++)" ;;
    esac
  fi

  if [ -n "$build_need" ] && [ -n "$install_cmd" ]; then
    sh -c "$install_cmd$build_need" || warn "installing$build_need failed"
  fi

  # the source: a git checkout kept up to date when git is there, an
  # archive of the latest source otherwise
  llama_src="${XDG_DATA_HOME:-$HOME/.local/share}/armin/llama.cpp"
  llama_repo="https://github.com/ggml-org/llama.cpp"
  mkdir -p "$(dirname "$llama_src")"
  if [ -d "$llama_src/.git" ] && command -v git >/dev/null 2>&1; then
    git -C "$llama_src" pull --ff-only -q
  elif command -v git >/dev/null 2>&1; then
    rm -rf "$llama_src"
    git clone -q --depth 1 "$llama_repo" "$llama_src"
  else
    rm -rf "$llama_src"
    mkdir -p "$llama_src"
    fetch "$llama_repo/archive/refs/heads/master.tar.gz" |
      tar -xz --strip-components=1 -C "$llama_src"
  fi

  gpu=""
  if command -v nvcc >/dev/null 2>&1; then
    gpu="-DGGML_CUDA=ON"
    ok "building llama-server with CUDA"
  elif command -v nvidia-smi >/dev/null 2>&1; then
    warn "an NVIDIA GPU, but no CUDA toolkit (nvcc): building for the CPU"
    echo "      install the CUDA toolkit and remove $llama_src/build, then"
    echo "      re-run this script, for a llama-server that runs on the GPU"
  fi

  if cmake -S "$llama_src" -B "$llama_src/build" -DCMAKE_BUILD_TYPE=Release \
       -DBUILD_SHARED_LIBS=OFF -DLLAMA_CURL=OFF $gpu >/dev/null &&
     cmake --build "$llama_src/build" --config Release --target llama-server \
       -j "$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)" >/dev/null; then
    cp "$llama_src/build/bin/llama-server" "$prefix/llama-server.new"
    mv -f "$prefix/llama-server.new" "$prefix/llama-server"
    ok "built llama-server: $prefix/llama-server"
  else
    err "building llama-server failed: see https://github.com/ggml-org/llama.cpp"
  fi
else
  warn "no llama-server: local models won't run (a hosted model or a"
  echo "      llama_url pointing at a server elsewhere still works; see"
  echo "      docs/guide/models.md)"
fi

# --- Optional: docker (web_search) --------------------------------------

if command -v docker >/dev/null 2>&1; then
  ok "docker found — web_search will work"
else
  warn "docker not found — web_search will report a clean \"unavailable\" error (everything else still works)"
fi

# --- universal-ctags: the repo map and nvim's symbol features ----------
# Debian names it ctags-universal; Fedora and Arch ship it as plain
# ctags, which elsewhere is often Emacs's incompatible one.

uctags=""
for b in universal-ctags ctags-universal; do
  if command -v "$b" >/dev/null 2>&1; then
    uctags=$(command -v "$b")
    break
  fi
done

if [ -z "$uctags" ] && [ -x "$HOME/.local/bin/universal-ctags" ]; then
  uctags="$HOME/.local/bin/universal-ctags"
fi

if [ -z "$uctags" ] && command -v ctags >/dev/null 2>&1 &&
   ctags --version 2>/dev/null | grep -q "Universal Ctags"; then
  uctags=$(command -v ctags)
fi

if [ -n "$uctags" ]; then
  ok "universal-ctags found ($uctags): the repo map and -lsp symbols work"
else
  case "$pm" in
    apt-get|brew) ctags_pkg=universal-ctags ;;
    *)            ctags_pkg=ctags ;;
  esac

  warn "universal-ctags not found"
  offer "universal-ctags" " $ctags_pkg" "
        - the repo map in the prompt, and -lsp's go-to-definition"
fi

# --- Starter config -------------------------------------------------------

env_path="$config_dir/.env"

if [ "$write_config" -eq 1 ]; then
  mkdir -p "$config_dir"

  if [ -e "$config_path" ]; then
    ok "config.conf already exists at $config_path — left untouched"
  else
    cat > "$config_path" <<'EOF'
# key = value, "#" comments, nothing fancier — see docs/guide/config.md;
# docs/guide/models.md for models.<name> blocks, and config.conf.example
# in armin's repository for every key, explained.
model     = chat
llama_url = http://localhost:8080
EOF
    ok "wrote a starter config to $config_path"
  fi

  # .env holds API keys for any models.<name> pointed at an
  # authenticated remote endpoint (see docs/guide/models.md, "Authenticated /
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

# --- System RAM ----------------------------------------------------------
# ram = <type>-<speed> in config.conf: armin -install estimates how
# fast a model generates from the RAM's bandwidth when weights don't
# all fit in VRAM. Linux only shows the type and speed to root
# (SMBIOS), so it's read here once with dmidecode — via sudo when
# you agree — or asked for, and kept in config.conf.

# ram_from_dmidecode prints "ddr5-5600", "ddr4-3200 1ch", … from
# dmidecode -t 17 on stdin, or nothing.
ram_from_dmidecode() {
  awk '
    /^Memory Device/ { size = ""; type = ""; speed = "" }
    /^[[:space:]]+Size:/ { size = $2 }
    /^[[:space:]]+Type:/ { type = $2 }
    /^[[:space:]]+Configured Memory Speed:/ && $4 ~ /^[0-9]+$/ { speed = $4 }
    /^[[:space:]]+Speed:/ && speed == "" && $2 ~ /^[0-9]+$/ { speed = $2 }
    function flush() {
      if (size ~ /^[0-9]+$/ && type != "" && type != "Unknown" && speed != "") {
        n++; t = type
        if (s == "" || speed + 0 < s + 0) s = speed
      }
      size = ""
    }
    /^$/ { flush() }
    END {
      flush()
      if (n == 0) exit
      out = tolower(t) "-" s
      if (n == 1 && t !~ /^LP/) out = out " 1ch"
      print out
    }
  '
}

valid_ram() {
  printf '%s\n' "$1" | grep -Eiq '^(lp)?ddr[0-9]+x?-[0-9]{3,5}( [0-9]+ch)?$'
}

echo
echo "system RAM (for armin -install):"
have_ram=$(sed -n 's/^[[:space:]]*ram[[:space:]]*=[[:space:]]*//p' "$config_path" 2> /dev/null | tail -1)

if [ -n "$have_ram" ] && [ -z "$ram" ]; then
  ok "ram = $have_ram in config.conf"
else
  if [ -z "$ram" ] && command -v dmidecode >/dev/null 2>&1; then
    if [ "$(id -u)" -eq 0 ]; then
      ram=$(dmidecode -t 17 2> /dev/null | ram_from_dmidecode)
    elif [ -t 0 ]; then
      printf '  read the RAM type and speed with sudo dmidecode? [Y/n] '
      read -r answer
      case "$answer" in
        n|N|no) ;;
        *) ram=$(sudo dmidecode -t 17 2> /dev/null | ram_from_dmidecode) ;;
      esac
    fi
  fi

  if [ -z "$ram" ] && [ -t 0 ]; then
    printf '  RAM type and speed, e.g. ddr5-5600, lpddr5x-7500, ddr4-3200 1ch (Enter to skip): '
    read -r ram
  fi

  if [ -z "$ram" ]; then
    warn "not set — armin -install will assume a speed; add ram = ddr5-5600 (yours) to $config_path"
  elif ! valid_ram "$ram"; then
    warn "\"$ram\" isn't TYPE-SPEED like ddr5-5600 — not saved"
  else
    mkdir -p "$config_dir"
    if [ -n "$have_ram" ]; then
      sed -i.bak "s/^[[:space:]]*ram[[:space:]]*=.*/ram = $ram/" "$config_path" && rm -f "$config_path.bak"
    else
      printf '\n# system RAM, for armin -install'"'"'s speed estimates\nram = %s\n' "$ram" >> "$config_path"
    fi
    ok "ram = $ram saved to $config_path"
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
case "$library_dir" in
  "~"|"~/"*) library_dir="$HOME${library_dir#\~}" ;;
esac

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
