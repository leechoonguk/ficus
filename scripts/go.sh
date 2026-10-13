#!/bin/sh
# Print the go command the Makefile builds with.
#
# Go 1.21 or newer on PATH is used as is: the Makefile sets GOTOOLCHAIN=auto,
# so it fetches the version go.mod asks for by itself (distributions such as
# Fedora and Alpine default to GOTOOLCHAIN=local, which would refuse). With no
# go, or one older than 1.21 (Ubuntu 22.04, Debian 12), the go.mod version is
# downloaded once from go.dev, checked against its published SHA-256, and kept
# in .cache/ of this repository; it is used only to build ficus.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
die() { echo "ficus: $*" >&2; exit 1; }

want=$(awk '$1 == "go" { print $2; exit }' "$root/go.mod")
[ -n "$want" ] || die "no go version in go.mod"

if have=$(command -v go 2>/dev/null); then
	v=$("$have" version 2>/dev/null | awk '{ print $3 }')  # go version go1.22.2 linux/amd64
	minor=$(echo "$v" | sed -n 's/^go1\.\([0-9][0-9]*\).*/\1/p')
	if [ -n "$minor" ] && [ "$minor" -ge 21 ]; then
		echo "$have"
		exit 0
	fi
	found="${v:-an unknown version} at $have"
else
	found="no go command"
fi

dir=$root/.cache/go$want
if [ -x "$dir/bin/go" ]; then
	echo "$dir/bin/go"
	exit 0
fi

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
FreeBSD) os=freebsd ;;
*) die "no Go download for $(uname -s); install Go $want from https://go.dev/dl/" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
i386 | i686) arch=386 ;;
armv6l | armv7l) arch=armv6l ;;
*) die "no Go download for $(uname -m); install Go $want from https://go.dev/dl/" ;;
esac

file=go$want.$os-$arch.tar.gz
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		die "building ficus needs Go $want ($found here); install curl or wget to download it, or install Go from https://go.dev/dl/"
	fi
}

echo "ficus: building needs Go $want ($found here); downloading $file into .cache/ (only used to build ficus)" >&2
mkdir -p "$root/.cache"
tmp=$(mktemp -d "$root/.cache/tmp.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT TERM
fetch "https://dl.google.com/go/$file" "$tmp/$file" || die "download of $file failed"
fetch "https://dl.google.com/go/$file.sha256" "$tmp/sha256" || die "download of $file.sha256 failed"
want_sum=$(tr -d ' \n' <"$tmp/sha256")
if command -v sha256sum >/dev/null 2>&1; then
	got_sum=$(sha256sum "$tmp/$file" | awk '{ print $1 }')
else
	got_sum=$(shasum -a 256 "$tmp/$file" | awk '{ print $1 }')
fi
[ "$got_sum" = "$want_sum" ] || die "checksum mismatch for $file"
tar -xzf "$tmp/$file" -C "$tmp"
mv "$tmp/go" "$dir"
echo "$dir/bin/go"
