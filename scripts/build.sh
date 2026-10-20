#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
mkdir -p dist

for target in windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
    target_os=${target%/*}
    target_arch=${target#*/}
    suffix=
    if [ "$target_os" = windows ]; then
        suffix=.exe
    fi
    output="dist/filelist-$target_os-$target_arch$suffix"
    printf 'Building %s\n' "$output"
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -o "$output" ./cmd/filelist
done
