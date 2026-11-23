#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

if [[ "$(uname -s)" != "Darwin" ]]; then
  printf 'This local build script requires macOS. Use bun run build for other platforms.\n' >&2
  exit 1
fi

for tool in go bun node codesign; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'Missing required tool: %s. Install it and run this script again.\n' "$tool" >&2
    exit 1
  fi
done

# Local builds must use the host, even if cross-compilation variables are set.
unset GOOS GOARCH
export CGO_ENABLED=0
arch="$(go env GOHOSTARCH)"
app_path="$project_root/build/darwin-$arch/GoMail.app"

printf 'Building GoMail locally for darwin/%s\n' "$arch"
bun install --frozen-lockfile

# MyGO generates bindings and builds the frontend, then stages the bundle before
# replacing the previous build. OAuth credentials are never embedded at build time.
bun run build -platform "darwin/$arch" -o build -skip-dmg -skip-notarize -sign -

printf '\nVerifying app signature...\n'
codesign --verify --deep --strict "$app_path"
printf '\nLocal build ready: %s\nRun: open "%s"\n' "$app_path" "$app_path"
