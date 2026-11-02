#!/usr/bin/env bash
set -euo pipefail
umask 022

# Usage: scripts/release.sh v0.11.0 [amd64|arm64 ...]
# Build on Linux with Go, GNU tar, gzip and sha256sum. No ZFS or root access needed.
chrona_version=${1:-}
chrona_version=${chrona_version#v}
if [[ ! "$chrona_version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
    printf 'Usage: %s vMAJOR.MINOR.PATCH[-prerelease] [amd64|arm64 ...]\n' "$0" >&2
    exit 2
fi
shift
if (( $# == 0 )); then
    set -- amd64 arm64
fi
for chrona_arch in "$@"; do
    case "$chrona_arch" in
        amd64|arm64) ;;
        *) printf 'Unsupported Linux architecture: %s\n' "$chrona_arch" >&2; exit 2 ;;
    esac
done

chrona_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$chrona_root"
chrona_dist=${CHRONA_DIST:-dist}
mkdir -p -- "$chrona_dist"
chrona_dist=$(cd -- "$chrona_dist" && pwd)
chrona_commit=$(git rev-parse HEAD)
chrona_epoch=$(git show -s --format=%ct HEAD)
chrona_go=$(go version)
chrona_stage=$(mktemp -d)
trap 'rm -rf -- "$chrona_stage"' EXIT
chrona_archives=()

for chrona_arch in "$@"; do
    chrona_name="chrona_${chrona_version}_linux_${chrona_arch}"
    chrona_bundle="$chrona_stage/$chrona_name"
    mkdir -p -- "$chrona_bundle/deploy" "$chrona_bundle/docs/images"
    CGO_ENABLED=0 GOOS=linux GOARCH="$chrona_arch" \
        go build -mod=readonly -buildvcs=false -trimpath \
        -ldflags="-s -w -X homelab/chrona/internal/buildinfo.Version=$chrona_version -X homelab/chrona/internal/buildinfo.Commit=$chrona_commit" \
        -o "$chrona_bundle/chrona" ./cmd/chrona
    install -m 644 deploy/chrona.service "$chrona_bundle/chrona.service"
    install -m 644 deploy/chrona.service "$chrona_bundle/deploy/chrona.service"
    install -m 644 deploy/chrona.service "$chrona_bundle/chrona.service.example"
    install -m 644 config.example.yaml LICENSE README.md README.zh-CN.md "$chrona_bundle/"
    install -m 644 docs/images/*.png "$chrona_bundle/docs/images/"
    printf 'Version: %s\nCommit: %s\nTarget: linux/%s\nGo: %s\nCGO_ENABLED: 0\n' \
        "$chrona_version" "$chrona_commit" "$chrona_arch" "$chrona_go" > "$chrona_bundle/BUILDINFO.txt"
    chmod 755 "$chrona_bundle/chrona"
    # Stable timestamps, ordering, ownership and gzip headers for repeatable archives.
    tar --sort=name --mtime="@$chrona_epoch" --owner=0 --group=0 --numeric-owner \
        -C "$chrona_stage" -cf - "$chrona_name" | gzip -n > "$chrona_dist/$chrona_name.tar.gz"
    chrona_archives+=("$chrona_name.tar.gz")
    printf 'Built %s\n' "$chrona_dist/$chrona_name.tar.gz"
done

(
    cd -- "$chrona_dist"
    sha256sum -- "${chrona_archives[@]}" > SHA256SUMS
    sha256sum -c SHA256SUMS
)
