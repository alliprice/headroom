#!/usr/bin/env bash
# Build headroom-exporter from a committed revision and swap it in under
# launchd.
#
# usage: scripts/install-exporter.sh [revision]   (default HEAD)
#
# The build comes from `git archive`, so uncommitted changes never ship and
# the binary's -version names exactly what it was built from. The new binary
# is staged in ~/.local/bin and renamed over the old one, which is atomic on
# one filesystem, then the launchd agent is restarted.
set -euo pipefail

label="com.alliprice.headroom-exporter"
bindir="$HOME/.local/bin"
target="$bindir/headroom-exporter"

repo="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
rev="$(git -C "$repo" rev-parse --verify "${1:-HEAD}^{commit}")"

work="$(mktemp -d)"
staged=""
cleanup() {
	rm -rf "$work"
	if [ -n "$staged" ]; then
		rm -f "$staged"
	fi
}
trap cleanup EXIT

mkdir "$work/src"
git -C "$repo" archive "$rev" | tar -x -C "$work/src"
(
	cd "$work/src"
	go build -trimpath -ldflags "-X main.revision=$rev" -o "$work/headroom-exporter" ./cmd/exporter
)

mkdir -p "$bindir"
staged="$(mktemp "$bindir/.headroom-exporter.XXXXXX")"
cp "$work/headroom-exporter" "$staged"
chmod 755 "$staged"

reported="$("$staged" -version)"
if [ "$reported" != "$rev" ]; then
	echo "built binary reports revision $reported, want $rev" >&2
	exit 1
fi

mv -f "$staged" "$target"
staged=""

launchctl kickstart -k "gui/$(id -u)/$label"
echo "installed $target at $rev and restarted $label"
