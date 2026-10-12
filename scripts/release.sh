#!/bin/sh
# Release ficus: `make release bump=patch|minor|major` (rule://release in .omp/rules).
#
# The version is the git tag. This script checks that the commit may be
# released, tags it with the next version, waits for the release workflow to
# publish it, checks the uploaded files, and installs the published release on
# Linux, macOS and Windows (verify-release.yml). It stops at the first failure.
#
#   release.sh patch|minor|major       release
#   DRY_RUN=1 release.sh patch         every check, no tag (prints the plan)
#   release.sh --unrelease vX.Y.Z      delete a release and its tag (rollback)
set -eu

repo=leechoonguk/ficus
# What .goreleaser.yaml publishes; check-release keeps this list in step.
assets="checksums.txt
ficus_amd64.deb
ficus_amd64.rpm
ficus_arm64.deb
ficus_arm64.rpm
ficus_darwin_amd64.tar.gz
ficus_darwin_arm64.tar.gz
ficus_linux_amd64.tar.gz
ficus_linux_arm64.tar.gz
ficus_windows_amd64.zip
ficus_windows_arm64.zip
install.ps1
install.sh"

say() { printf '==> %s\n' "$*"; }
die() { printf 'release: %s\n' "$*" >&2; exit 1; }

# The run id of the newest run of workflow $1 for ref $2 (branch or tag)
# created at or after $3 (UTC, RFC 3339), polling until it appears.
find_run() {
	i=0
	while [ $i -lt 60 ]; do
		id=$(gh run list -R "$repo" --workflow "$1" --branch "$2" --limit 5 \
			--json databaseId,createdAt --jq "[.[] | select(.createdAt >= \"$3\")][0].databaseId // empty")
		[ -n "$id" ] && { echo "$id"; return; }
		sleep 5
		i=$((i + 1))
	done
	die "no $1 run for $2 appeared within 5 minutes"
}

unrelease() {
	v=$1
	case $v in v[0-9]*.[0-9]*.[0-9]*) ;; *) die "usage: make unrelease v=vX.Y.Z" ;; esac
	say "deleting release $v and its tag"
	gh release delete "$v" -R "$repo" --yes --cleanup-tag 2>/dev/null || say "no GitHub release $v"
	git push origin ":refs/tags/$v" 2>/dev/null || true
	git tag -d "$v" 2>/dev/null || true
	latest=$(gh release view -R "$repo" --json tagName --jq .tagName 2>/dev/null || echo none)
	say "done; releases/latest now points at $latest"
}

[ "${1:-}" = --unrelease ] && { unrelease "${2:-}"; exit 0; }

bump=${1:-}
case $bump in patch | minor | major) ;; *) die "usage: make release bump=patch|minor|major" ;; esac
command -v gh >/dev/null 2>&1 || die "gh (GitHub CLI) is required"
gh auth status >/dev/null 2>&1 || die "gh is not logged in (gh auth login)"

# ── 1. The commit may be released ────────────────────────────────────────────
branch=$(git rev-parse --abbrev-ref HEAD)
[ "$branch" = main ] || die "on branch '$branch'; releases are cut from main"
[ -z "$(git status --porcelain)" ] || die "the worktree has changes; commit or stash them first"
git fetch -q origin main --tags
sha=$(git rev-parse HEAD)
short=$(git rev-parse --short HEAD)
[ "$sha" = "$(git rev-parse origin/main)" ] || die "HEAD is not origin/main; push (or pull) first"

say "CI for $short"
ci=$(gh run list -R "$repo" --workflow ci.yml --commit "$sha" --limit 1 --json databaseId,status,conclusion \
	--jq '.[] | "\(.databaseId) \(.status) \(.conclusion)"')
[ -n "$ci" ] || die "no CI run for this commit; push it and let CI run"
set -- $ci
if [ "$2" != completed ]; then
	say "CI is still running; waiting"
	gh run watch "$1" -R "$repo" --exit-status >/dev/null || die "CI failed for this commit"
elif [ "$3" != success ]; then
	die "CI $3 for this commit"
fi

# ── 2. The next version ──────────────────────────────────────────────────────
last=$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1)
[ -n "$last" ] || last=v0.0.0
IFS=. read -r major minor patch <<EOF
${last#v}
EOF
case $bump in
major) major=$((major + 1)) minor=0 patch=0 ;;
minor) minor=$((minor + 1)) patch=0 ;;
patch) patch=$((patch + 1)) ;;
esac
next=v$major.$minor.$patch
git rev-parse -q --verify "refs/tags/$next" >/dev/null && die "tag $next already exists"
if [ "$last" != v0.0.0 ] && [ "$(git rev-list -n 1 "$last")" = "$sha" ]; then
	die "this commit is already released as $last"
fi

say "$last -> $next ($bump)"
if [ "$last" = v0.0.0 ]; then
	git --no-pager log --oneline --no-decorate
else
	git --no-pager log --oneline --no-decorate "$last..HEAD"
fi

if [ "${DRY_RUN:-}" = 1 ]; then
	say "dry run: would tag $next on $short and publish it"
	exit 0
fi

# ── 3. Tag and publish ───────────────────────────────────────────────────────
since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
git tag -a "$next" -m "ficus $next"
git push -q origin "$next"
say "tag $next pushed; waiting for the release workflow"
run=$(find_run release.yml "$next" "$since")
if ! gh run watch "$run" -R "$repo" --exit-status >/dev/null; then
	die "release workflow failed: https://github.com/$repo/actions/runs/$run
  fix it and release the next version, or roll back with: make unrelease v=$next"
fi

# ── 4. The published files ───────────────────────────────────────────────────
got=$(gh release view "$next" -R "$repo" --json assets --jq '.assets[].name')
missing=
for a in $assets; do
	printf '%s\n' "$got" | grep -qxF "$a" || missing="$missing $a"
done
[ -z "$missing" ] || die "release $next is missing:$missing (https://github.com/$repo/releases/tag/$next)"
say "release $next has all $(printf '%s\n' "$assets" | wc -l | tr -d ' ') files"

# ── 5. Install it like users do ──────────────────────────────────────────────
since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
gh workflow run verify-release.yml -R "$repo" --ref main -f version="$next" >/dev/null
say "installing $next on Linux, macOS and Windows (verify-release)"
run=$(find_run verify-release.yml main "$since")
if ! gh run watch "$run" -R "$repo" --exit-status >/dev/null; then
	die "installing the release failed: https://github.com/$repo/actions/runs/$run
  the release is public; fix and release the next version, or roll back with: make unrelease v=$next"
fi
gh run view "$run" -R "$repo" --json jobs --jq '.jobs[] | "    \(.conclusion)  \(.name)"'
say "released $next: https://github.com/$repo/releases/tag/$next"
