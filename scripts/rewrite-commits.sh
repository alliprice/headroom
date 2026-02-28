#!/usr/bin/env bash
#
# Rewrites commit messages using a JSON mapping file.
# Usage: ./scripts/rewrite-commits.sh [--dry-run]
#
# The mapping file (scripts/commit-messages.json) maps old subject lines
# to new ones. Commits whose subjects don't appear in the mapping are
# left unchanged.
#
# After rewriting, you'll need to force-push:
#   git push origin main --force

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MAPPING="$SCRIPT_DIR/commit-messages.json"
DRY_RUN=false

if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
fi

if [[ ! -f "$MAPPING" ]]; then
    echo "error: mapping file not found: $MAPPING" >&2
    exit 1
fi

if ! command -v python3 &>/dev/null; then
    echo "error: python3 required" >&2
    exit 1
fi

# Build a shell-safe filter script from the JSON mapping.
# Python reads the JSON and emits a shell case statement.
FILTER_SCRIPT=$(python3 -c "
import json, sys, shlex

with open(sys.argv[1]) as f:
    mapping = json.load(f)

print('first_line=\$(head -1)')
print('case \"\$first_line\" in')
for old, new in mapping.items():
    # Shell-escape both sides
    old_escaped = old.replace(\"'\", \"'\\\\'\")
    new_escaped = new.replace('\\\\', '\\\\\\\\')
    print(f\"  '{old_escaped}')\" )
    print(f'    printf \"%s\" {shlex.quote(new)}')
    print('    ;;')
print('  *)')
print('    printf \"%s\" \"\$first_line\"')
print('    # Preserve body if present')
print('    rest=\$(cat)')
print('    if [ -n \"\$rest\" ]; then')
print('      printf \"\\n%s\" \"\$rest\"')
print('    fi')
print('    ;;')
print('esac')
" "$MAPPING")

if $DRY_RUN; then
    echo "=== Dry run: commits that would be rewritten ==="
    echo ""
    git log --reverse --format="%s" | while IFS= read -r subject; do
        new=$(python3 -c "
import json, sys
with open(sys.argv[1]) as f:
    m = json.load(f)
subj = sys.argv[2]
if subj in m:
    print(m[subj])
" "$MAPPING" "$subject" 2>/dev/null || true)
        if [[ -n "$new" && "$new" != "$subject" ]]; then
            echo "  OLD: $subject"
            echo "  NEW: $new"
            echo ""
        fi
    done
    echo "=== Run without --dry-run to apply ==="
    exit 0
fi

echo "Rewriting commit messages..."
FILTER_BRANCH_SQUELCH_WARNING=1 git filter-branch --msg-filter "$FILTER_SCRIPT" -- --all

echo ""
echo "Done. Verify with: git log --oneline"
echo "Then force-push:   git push origin main --force"
