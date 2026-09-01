#!/usr/bin/env bash
set -Eeuo pipefail

VERSION="${1:-}"
[[ "${VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || { printf 'usage: %s vX.Y.Z\n' "$0" >&2; exit 2; }
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
COMMIT="$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || printf unknown)"

cd "${ROOT_DIR}"
if [[ "${SERVERPAL_SKIP_WEB_BUILD:-0}" != "1" ]]; then
  npm ci
  npm run build
fi
mkdir -p "${DIST_DIR}"

for ARCH in amd64 arm64; do
  STAGE="$(mktemp -d "${TMPDIR:-/tmp}/serverpal-release.XXXXXX")"
  mkdir -p "${STAGE}/systemd"
  GOOS=linux GOARCH="${ARCH}" CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o "${STAGE}/serverpal" ./cmd/serverpal
  install -m 0755 scripts/serverpal-update "${STAGE}/serverpal-update"
  cp packaging/systemd/*.service "${STAGE}/systemd/"
  COPYFILE_DISABLE=1 tar --no-xattrs -czf "${DIST_DIR}/serverpal-linux-${ARCH}.tar.gz" -C "${STAGE}" .
  rm -rf "${STAGE}"
done

(cd "${DIST_DIR}" && if command -v sha256sum >/dev/null 2>&1; then sha256sum serverpal-linux-amd64.tar.gz serverpal-linux-arm64.tar.gz; else shasum -a 256 serverpal-linux-amd64.tar.gz serverpal-linux-arm64.tar.gz; fi > checksums.txt)
printf 'release artifacts written to %s\n' "${DIST_DIR}"
