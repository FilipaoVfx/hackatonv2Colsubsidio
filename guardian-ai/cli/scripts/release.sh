#!/usr/bin/env bash
# Builds and publishes the Secura CLI release.
#
# Windows only, by design: every other platform reaches the CLI through the
# browser terminal at teamflashackaton30x.com, which needs no install at all.
# The CLI has zero cgo, so this is a plain GOOS/GOARCH cross-compile — no
# goreleaser, no C toolchain.
set -euo pipefail

VERSION="${1:-v0.1.0}"
TAG="cli-${VERSION}"
REPO="FilipaoVfx/hackatonv2Colsubsidio"
CLI_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${CLI_DIR}/dist"

export PATH="$PATH:/usr/local/go/bin"

rm -rf "$DIST"
mkdir -p "$DIST"
cd "$CLI_DIR"

echo "==> building windows/amd64 ${VERSION}"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
  -trimpath \
  -ldflags "-s -w -X guardianai/cli/cmd.version=${VERSION}" \
  -o "${DIST}/secura.exe" .

cd "$DIST"
# python3 rather than the zip binary: it is already present everywhere this
# runs, and PowerShell's Expand-Archive reads a deflate zip either way.
python3 -c "
import zipfile
with zipfile.ZipFile('secura_windows_amd64.zip', 'w', zipfile.ZIP_DEFLATED) as z:
    z.write('secura.exe')
"
rm secura.exe
sha256sum secura_windows_amd64.zip > checksums.txt

echo "==> artifacts"
cat checksums.txt

echo "==> publishing ${TAG} to ${REPO}"
if gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
  gh release upload "$TAG" --repo "$REPO" --clobber \
    secura_windows_amd64.zip checksums.txt
else
  gh release create "$TAG" --repo "$REPO" \
    --title "Secura CLI ${VERSION}" \
    --notes "Secura CLI — Guardian AI Operations Center.

Instalación en Windows:

    irm https://teamflashackaton30x.com/install.ps1 | iex

Cualquier otro sistema: https://teamflashackaton30x.com → \"Probar en el navegador\".

El binario descubre el backend automáticamente desde teamflashackaton30x.com/secura-endpoint.json, así que no hace falta pasar --api-url." \
    secura_windows_amd64.zip checksums.txt
fi

echo "==> done: https://github.com/${REPO}/releases/tag/${TAG}"
