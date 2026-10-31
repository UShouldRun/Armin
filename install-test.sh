#!/bin/sh
# install-test.sh builds this checkout as armin-test, a second armin
# that lives beside the installed one and never touches it: its own
# binary (~/.local/bin/armin-test), config (~/.config/armin-test) and
# data (~/.local/share/armin-test: sessions, memories, the knowledge
# trees, metrics, and the socket armins join through, so it never
# joins or hosts the installed armin's sessions). Use it to try a
# branch for a while; ./install.sh stays the way to install armin.
#
# The first run seeds armin-test's config: config.conf.restructure
# from this checkout when there is one, else a copy of the installed
# armin's config.conf; the API keys from the installed .env (copied,
# mode 600); the skills linked, read-only in practice. Later runs only
# rebuild the binary: edit ~/.config/armin-test/config.conf directly.
#
# Usage: ./install-test.sh [--name NAME] [--prefix DIR]
#
#   --name NAME    the program and folder name (default: armin-test)
#   --prefix DIR   install the binary here (default: ~/.local/bin)

set -eu

name="armin-test"
prefix="${HOME}/.local/bin"

while [ $# -gt 0 ]; do
  case "$1" in
    --name) name="$2"; shift 2 ;;
    --prefix) prefix="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

if [ "$name" = "armin" ]; then
  echo "refusing --name armin: that is the installed armin's (use ./install.sh)" >&2
  exit 2
fi

cd "$(dirname "$0")"

config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
main_conf="$config_home/armin"
test_conf="$config_home/$name"

echo "building ./cmd/armin as $name ($(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?'))..."
go build -ldflags "-X armin/internal/build.Name=$name" -o "$name" ./cmd/armin

mkdir -p "$prefix"
# Copy then rename: renaming over a running binary is fine, while
# writing into it fails with "Text file busy".
cp "$name" "$prefix/$name.new"
mv -f "$prefix/$name.new" "$prefix/$name"
chmod +x "$prefix/$name"
rm -f "$name"
echo "installed to $prefix/$name"

mkdir -p "$test_conf"
chmod 700 "$test_conf"

if [ ! -f "$test_conf/config.conf" ]; then
  if [ -f config.conf.restructure ]; then
    cp config.conf.restructure "$test_conf/config.conf"
    echo "config: config.conf.restructure → $test_conf/config.conf"
  elif [ -f "$main_conf/config.conf" ]; then
    cp "$main_conf/config.conf" "$test_conf/config.conf"
    echo "config: a copy of $main_conf/config.conf"
  fi
fi

if [ ! -f "$test_conf/.env" ] && [ -f "$main_conf/.env" ]; then
  cp "$main_conf/.env" "$test_conf/.env"
  chmod 600 "$test_conf/.env"
  echo "keys: a copy of $main_conf/.env"
fi

if [ ! -e "$test_conf/skills" ] && [ -d "$main_conf/skills" ]; then
  ln -s "$main_conf/skills" "$test_conf/skills"
  echo "skills: linked to $main_conf/skills"
fi

echo "run it as: $name ."
