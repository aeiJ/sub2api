#!/usr/bin/env bash
# 本地构建镜像的快速脚本，避免在命令行反复输入构建参数。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

image="${IMAGE:-sub2api}"
tag="${TAG:-latest}"
platform="${PLATFORM:-linux/amd64}"
push="${PUSH:-0}"

output_flag="--load"
if [[ "${push}" == "1" || "${push}" == "true" ]]; then
    output_flag="--push"
fi

docker buildx build \
    --platform "${platform}" \
    ${output_flag} \
    -t "${image}:${tag}" \
    --build-arg GOPROXY=https://goproxy.cn,direct \
    --build-arg GOSUMDB=sum.golang.google.cn \
    -f "${REPO_ROOT}/Dockerfile" \
    "${REPO_ROOT}"
