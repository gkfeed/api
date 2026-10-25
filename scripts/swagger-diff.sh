#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    echo "usage: $0 BASE_REF" >&2
    exit 2
fi

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"
base_commit=$(git rev-parse --verify "$1^{commit}")
mkdir -p .cache
work_dir=$(mktemp -d "$repo_dir/.cache/swagger-diff.XXXXXX")
report_dir="$repo_dir/.cache/swagger-report"
mkdir -p "$report_dir"

# An archive also works before generated docs were removed from the base branch.
# Use a file rather than a pipeline so a failed git archive cannot be hidden.
git archive --output="$work_dir/base.tar" "$base_commit" app
tar -xf "$work_dir/base.tar" -C "$work_dir"
./scripts/swagger.sh generate "$work_dir/app" "$report_dir/base"
./scripts/swagger.sh generate "$repo_dir/app" "$report_dir/head"
./scripts/swagger.sh validate "$report_dir/base/swagger.json"
./scripts/swagger.sh validate "$report_dir/head/swagger.json"
printf 'Base source: %s\nHead source: %s\nGenerator: scripts/swagger.sh from head\n' \
    "$base_commit" "$(git rev-parse HEAD)" > "$report_dir/revisions.txt"

# Preserve the nonzero exit status for incompatible contracts. CI publishes this
# as an advisory report while the initial generated baseline is being adopted.
status=0
./scripts/swagger.sh diff "$report_dir/base/swagger.json" "$report_dir/head/swagger.json" \
    > "$report_dir/breaking.txt" 2>&1 || status=$?
cat "$report_dir/breaking.txt"
exit "$status"
