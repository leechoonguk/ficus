#!/bin/sh
# Install ficus on Linux or macOS from a GitHub release and enable it in
# every supported shell:
#
#   curl -fsSL https://github.com/leechoonguk/ficus/releases/latest/download/install.sh | sh
#
# Environment:
#   FICUS_VERSION      release tag to install (default: the latest)
#   FICUS_INSTALL_DIR  where the binaries go (default: ~/.local/bin)
#   FICUS_SHELLS       shells to enable, e.g. "zsh,bash" (default: all found)
#   FICUS_NO_SETUP=1   only install the binaries; run `ficus setup` yourself
#
# Uninstall: ficus setup --remove; rm ~/.local/bin/ficus ~/.local/bin/ficus-carapace
set -eu

repo=leechoonguk/ficus
dir=${FICUS_INSTALL_DIR:-$HOME/.local/bin}

die() { echo "ficus: $*" >&2; exit 1; }

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "unsupported system $(uname -s); on Windows use install.ps1" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported CPU $(uname -m)" ;;
esac

if [ -n "${FICUS_VERSION:-}" ]; then
	base=https://github.com/$repo/releases/download/$FICUS_VERSION
else
	base=https://github.com/$repo/releases/latest/download
fi
archive=ficus_${os}_${arch}.tar.gz

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		die "need curl or wget"
	fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "ficus: downloading $base/$archive"
fetch "$base/$archive" "$tmp/$archive" || die "download failed"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "download of checksums.txt failed"

want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$archive is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$archive" | awk '{ print $1 }')
else
	got=$(shasum -a 256 "$tmp/$archive" | awk '{ print $1 }')
fi
[ "$got" = "$want" ] || die "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$dir"
for f in ficus ficus-carapace; do
	# Replace, not overwrite: a running ficus keeps its old file.
	cp "$tmp/$f" "$dir/.$f.new"
	chmod 755 "$dir/.$f.new"
	mv -f "$dir/.$f.new" "$dir/$f"
done
echo "ficus: installed $("$dir/ficus" version) in $dir"

if [ "${FICUS_NO_SETUP:-}" = 1 ]; then
	echo "ficus: run '$dir/ficus setup' to enable it in your shells"
elif [ -n "${FICUS_SHELLS:-}" ]; then
	"$dir/ficus" setup --shells "$FICUS_SHELLS"
else
	"$dir/ficus" setup || echo "ficus: run '$dir/ficus setup --shells ...' once a supported shell is installed" >&2
fi
