#!/usr/bin/env bash
# Keep the PRD repository (domainlang) identical to this one — DEV (domain) —
# except for the repository's own name.
#
#   scripts/sync-prd.sh check [PRD_DIR]   report any difference beyond the name
#   scripts/sync-prd.sh sync  [PRD_DIR]   make PRD match DEV; does not commit
#
# PRD_DIR defaults to ../domainlang. Run it from the DEV checkout; the Makefile
# targets `check-prd` and `sync-prd` are the usual way in.
#
# The one rewrite is the repository path in URLs, `RFuller25/domainlang` becoming
# `RFuller25/domainlang`. Nothing else is allowed to differ: if PRD needs a
# change, make it in DEV first (it has no business being PRD-only), then sync.
# That is also why `sync` refuses a PRD with uncommitted changes — they would
# be overwritten — and why it lists every PRD file it is about to overwrite
# that is not merely a stale copy of DEV's. A PRD edit that DEV never saw shows
# up there; port it back to DEV before syncing, or it is lost.
#
# Tracked files only (`git ls-files`): build output and editor litter never
# cross. A file tracked in PRD but not in DEV is removed from PRD.
set -euo pipefail

mode=${1:-}
dev=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
prd=$(cd "${2:-$dev/../domainlang}" 2>/dev/null && pwd) || {
	echo "sync-prd: no PRD checkout at ${2:-$dev/../domainlang}" >&2
	exit 2
}
[ "$prd" != "$dev" ] || { echo "sync-prd: PRD and DEV are the same directory" >&2; exit 2; }

# The URL rewrite, byte-safe: some tracked files (docs/render.js) are not text
# as far as sed is concerned. perl is on every machine that has git.
to_prd() { perl -pe 's#RFuller25/domainlang(?!lang)#RFuller25/domainlang#g' "$1"; }

differs() { ! cmp -s <(to_prd "$dev/$1") "$prd/$1"; }

cd "$dev"
mapfile -t dev_files < <(git ls-files)
cd "$prd"
mapfile -t prd_files < <(git ls-files)

# Files each side has that the other has not.
only_prd=$(comm -13 <(printf '%s\n' "${dev_files[@]}" | sort) <(printf '%s\n' "${prd_files[@]}" | sort))

changed=()
for f in "${dev_files[@]}"; do
	if [ ! -e "$prd/$f" ] || differs "$f"; then changed+=("$f"); fi
done

case "$mode" in
check)
	if [ ${#changed[@]} -eq 0 ] && [ -z "$only_prd" ]; then
		echo "PRD matches DEV, apart from the repository name."
		exit 0
	fi
	[ ${#changed[@]} -eq 0 ] || { echo "differ (or missing from PRD):"; printf '  %s\n' "${changed[@]}"; }
	[ -z "$only_prd" ] || { echo "only in PRD:"; printf '%s\n' "$only_prd" | sed 's/^/  /'; }
	echo "run: make sync-prd   (after porting any PRD-only edit back to DEV)"
	exit 1
	;;
sync)
	if [ -n "$(git -C "$prd" status --porcelain)" ]; then
		echo "sync-prd: PRD has uncommitted changes; commit or stash them first" >&2
		exit 2
	fi
	for f in "${changed[@]}"; do
		mkdir -p "$(dirname "$prd/$f")"
		to_prd "$dev/$f" >"$prd/$f"
		if [ -x "$dev/$f" ]; then chmod +x "$prd/$f"; else chmod -x "$prd/$f"; fi
	done
	[ -z "$only_prd" ] || printf '%s\n' "$only_prd" | while read -r f; do rm -f "$prd/$f"; echo "removed $f"; done
	git -C "$prd" add -A
	echo "synced ${#changed[@]} file(s) into $prd — review with: git -C $prd status; then build, test and commit"
	;;
*)
	echo "usage: $0 check|sync [PRD_DIR]" >&2
	exit 2
	;;
esac
