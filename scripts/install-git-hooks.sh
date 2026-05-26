#!/usr/bin/env sh
# Point Git at versioned hooks under .githooks (per-clone config, not committed).
set -euo pipefail

cd "$(dirname "$0")/.." || exit 1

git config core.hooksPath .githooks
chmod +x .githooks/pre-commit 2>/dev/null || true
echo "Configured this clone: core.hooksPath=.githooks (pre-commit runs gofmt -l ., same as CI)."
