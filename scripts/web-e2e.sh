#!/usr/bin/env bash
# Runs gummi web's Playwright suite (e2e/web). Arguments pass straight to
# `playwright test`, e.g.:
#
#   scripts/web-e2e.sh                     # everything, every project
#   scripts/web-e2e.sh --project=harness   # prove the harness (no browser)
#   scripts/web-e2e.sh --project=desktop specs/pairing.spec.ts
#
# Needs Go, python3 and Node 22. In the dev container, a standalone Node and
# Chromium's libraries live under ~/.local/pwlibs; env.sh puts them on the
# path when it exists. GUMMI_E2E_KEEP=1 keeps each test's temp workspace.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
suite="$root/e2e/web"

if [ -f "$HOME/.local/pwlibs/env.sh" ]; then
    # shellcheck disable=SC1091
    . "$HOME/.local/pwlibs/env.sh"
fi

command -v node >/dev/null || { echo "web-e2e: node not found (need Node 22)" >&2; exit 1; }
command -v python3 >/dev/null || { echo "web-e2e: python3 not found (the scripted agent and fake gh need it)" >&2; exit 1; }

cd "$suite"
if [ ! -d node_modules ] || [ package-lock.json -nt node_modules/.package-lock.json ]; then
    npm ci --no-audit --no-fund
fi
# A no-op when the browser is already cached (~/.cache/ms-playwright).
npx playwright install chromium-headless-shell >/dev/null

exec npx playwright test "$@"
