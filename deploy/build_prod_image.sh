#!/usr/bin/env bash
# Build a production candidate image for linux/amd64 with immutable and candidate tags.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

release="${1:-}"
image="${2:-sub2api}"
platform="${PLATFORM:-linux/amd64}"
push="${PUSH:-0}"

if [[ -z "${release}" ]]; then
    echo "Usage: $0 <release-name> [image]" >&2
    echo "Example: $0 v0.1.143-prod.1 ghcr.io/owner/sub2api" >&2
    exit 2
fi

short_sha="$(git -C "${REPO_ROOT}" rev-parse --short=8 HEAD)"
immutable_tag="${release}-${short_sha}"

output_flag="--load"
if [[ "${push}" == "1" || "${push}" == "true" ]]; then
    output_flag="--push"
fi

docker buildx build \
    --platform "${platform}" \
    ${output_flag} \
    -t "${image}:${immutable_tag}" \
    -t "${image}:prod-candidate" \
    --build-arg GOPROXY=https://goproxy.cn,direct \
    --build-arg GOSUMDB=sum.golang.google.cn \
    -f "${REPO_ROOT}/Dockerfile" \
    "${REPO_ROOT}"

cat <<EOF
Built production candidate:
  ${image}:${immutable_tag}
  ${image}:prod-candidate

After validation, promote the immutable tag in deployment config or tag it as prod:
  docker tag ${image}:${immutable_tag} ${image}:prod
EOF
