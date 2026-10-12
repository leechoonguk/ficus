#!/bin/sh
# Sets up the throwaway environment docs/demo.tape records in: a small git
# repository and a zsh that loads only ficus (never your own ~/.zshrc).
#   docs/demo-setup.sh <ficus binary>
set -eu
ficus=$1
root=/tmp/ficus-demo
rm -rf "$root"
mkdir -p "$root/zdot" "$root/cache" "$root/project/docs" "$root/project/cmd"

cat >"$root/zdot/.zshrc" <<RC
PS1='%F{blue}%1~%f %F{green}❯%f '
export FICUS_CACHE_DIR=$root/cache
eval "\$('$ficus' init zsh)"
RC

cd "$root/project"
git init -q -b main
git config user.name demo
git config user.email demo@example.com
printf '# demo\n\nA small project for the ficus demo.\n' >README.md
printf 'package main\n\nfunc main() {}\n' >cmd/main.go
printf 'build:\n\tgo build ./...\n' >Makefile
printf 'Notes\n' >docs/notes.md
git add -A && git commit -q -m "first commit"
git branch feature/login
git branch fix/typo
git tag v1.0.0

# Fill the candidate cache so the recording does not wait for a cold start.
export FICUS_CACHE_DIR="$root/cache"
for line in 'git ch' 'git checkout ' 'ls -' 'ls -l'; do
	"$ficus" complete --shell zsh --budget 0 -- "$line" >/dev/null
done
