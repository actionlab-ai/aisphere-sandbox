#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || git rev-parse --short HEAD 2>/dev/null || echo dev)}"
ARCHES="${ARCHES:-amd64 arm64}"
OUT_DIR="${OUT_DIR:-dist}"
SKIP_IMAGES="${SKIP_IMAGES:-false}"
mkdir -p "$OUT_DIR"

for arch in $ARCHES; do
  work=".release/aisphere-sandbox-${VERSION}-${arch}"
  rm -rf "$work"
  mkdir -p "$work/bin" "$work/deployments" "$work/scripts" "$work/images"
  echo "building binary for linux/$arch"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "$work/bin/aisphere-sandbox-manager" ./cmd/sandbox-manager
  cp -a deployments "$work/"
  cp -a scripts/render-manifests.sh "$work/scripts/"
  cp README.md "$work/README.md"
  echo "$VERSION" > "$work/VERSION"
  echo "$arch" > "$work/ARCH"
  if [[ "$SKIP_IMAGES" != "true" ]]; then
    echo "image archives are not embedded by default; build images with scripts/build-images.sh"
  fi
  payload="$OUT_DIR/aisphere-sandbox-${VERSION}-${arch}.payload.tar.gz"
  tar -C .release -czf "$payload" "aisphere-sandbox-${VERSION}-${arch}"
  run="$OUT_DIR/aisphere-sandbox-${VERSION}-${arch}.run"
  cat > "$run" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
MARKER="__PAYLOAD__"
DEST="${1:-./aisphere-sandbox-offline}"
LINE=$(awk "/^${MARKER}$/ {print NR + 1; exit 0;}" "$0")
mkdir -p "$DEST"
tail -n +"$LINE" "$0" | tar -xz -C "$DEST" --strip-components=1
echo "unpacked to $DEST"
exit 0
__PAYLOAD__
SH
  cat "$payload" >> "$run"
  chmod +x "$run"
  rm -f "$payload"
  echo "created $run"
done
