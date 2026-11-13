#!/bin/sh
set -eu

if [ "$(uname -s)" != Darwin ]; then
  printf '%s\n' 'Run this packaging script on macOS with Xcode command-line tools installed.' >&2
  exit 1
fi
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
staging=$(mktemp -d)
trap 'rm -rf -- "$staging"' EXIT
trap 'exit 1' HUP INT TERM

go test -tags ci ./...
export MACOSX_DEPLOYMENT_TARGET=13.0
for architecture in arm64 amd64; do
  CGO_ENABLED=1 GOARCH="$architecture" go build -buildvcs=false -trimpath \
    -ldflags='-s -w' -o "$staging/MCPDashboard-$architecture" .
done
bundle="$staging/MCPDashboard.app"
mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources"
cp packaging/icon/MCPDashboard.icns "$bundle/Contents/Resources/"
cp packaging/Info.plist "$bundle/Contents/Info.plist"
/usr/bin/lipo -create "$staging/MCPDashboard-arm64" "$staging/MCPDashboard-amd64" \
  -output "$bundle/Contents/MacOS/MCPDashboard"
/usr/bin/codesign --force --sign - "$bundle"
/usr/bin/codesign --verify --strict "$bundle"
"$bundle/Contents/MacOS/MCPDashboard" --version
mkdir -p dist/macos
/usr/bin/ditto "$bundle" dist/macos/MCPDashboard.app
printf '%s\n' 'Built dist/macos/MCPDashboard.app for local testing.'
