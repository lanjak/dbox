#!/bin/sh
# Copyright (C) 2026 Jake Lancaster. SPDX-License-Identifier: GPL-3.0-or-later
# Build static dbox binaries for every supported platform into dist/,
# with a SHA256SUMS file. Usage: scripts/build-release.sh v1.2.3
set -eu

version=${1:?usage: scripts/build-release.sh <version>}
cd "$(dirname "$0")/.."
rm -rf dist
mkdir -p dist

for target in \
	linux/amd64 linux/arm64 linux/arm linux/386 linux/riscv64 \
	darwin/amd64 darwin/arm64 \
	freebsd/amd64 freebsd/arm64 \
	windows/amd64 windows/arm64; do
	os=${target%/*}
	arch=${target#*/}
	name="dbox-$os-$arch"
	[ "$os" = windows ] && name="$name.exe"
	echo "building $name"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch GOARM=7 \
		go build -trimpath -ldflags "-s -w -X main.version=$version" -o "dist/$name" ./cmd/dbox
done

cd dist
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum dbox-* > SHA256SUMS
else
	shasum -a 256 dbox-* > SHA256SUMS
fi
echo "done: binaries and SHA256SUMS are in dist/"
