# 离线重建与启动前后端服务教程

本文适用于当前仓库，目标是在没有外网的机器上从源码重新构建并启动 Sub2API。

先明确一个项目事实：生产模式下没有单独的前端服务。前端由 `frontend` 目录构建到 `backend/internal/web/dist`，再通过 Go 的 `-tags embed` 嵌入后端二进制，最终由后端在 `8080` 提供页面和 API。开发模式才会拆成后端 `8080` 和 Vite 前端 `3000`。

## 1. 离线前必须准备的东西

断网后不能拉取 Go modules、pnpm 包、Docker 基础镜像、Alpine apk 包。请先在有网环境准备离线包，或在目标机断网前完成缓存。

建议版本：

- Go `1.26.4`，与 `backend/go.mod` 和 Dockerfile 一致。
- Node.js `24`。
- pnpm `9`，Dockerfile 中固定使用 pnpm 9。
- Docker Compose v2 和 buildx，用 Docker 方案时需要。
- 如果使用根目录的 `start.sh`，目标机还需要 `zsh`、`screen`、`curl`、`lsof`。

## 2. 有网环境：准备源码依赖缓存

在仓库根目录执行：

```bash
export REPO="$PWD"
mkdir -p "$REPO/offline-cache/go/pkg/mod" \
         "$REPO/offline-cache/go/build" \
         "$REPO/offline-cache/pnpm/store" \
         "$REPO/offline-cache/images"
```

缓存 Go 依赖：

```bash
cd "$REPO/backend"
GOTOOLCHAIN=local \
GOMODCACHE="$REPO/offline-cache/go/pkg/mod" \
GOCACHE="$REPO/offline-cache/go/build" \
go mod download
```

缓存前端依赖：

```bash
cd "$REPO/frontend"
corepack enable
corepack prepare pnpm@9 --activate
pnpm fetch --frozen-lockfile --store-dir "$REPO/offline-cache/pnpm/store"
```

注意：pnpm store 只包含项目依赖，不包含 Node.js 和 pnpm 程序本身。目标机断网前必须已经能执行 `node` 和 `pnpm`，或者你需要单独携带对应系统的 Node.js/pnpm 安装包。

## 3. 有网环境：准备 Docker 离线包

如果断网后要用 Docker Compose 跑整套服务，先拉取服务镜像和构建基础镜像。部署服务器是 amd64 时使用 `linux/amd64`：

```bash
cd "$REPO"
docker pull --platform linux/amd64 node:24-alpine
docker pull --platform linux/amd64 golang:1.26.4-alpine
docker pull --platform linux/amd64 alpine:3.21
docker pull --platform linux/amd64 postgres:18-alpine
docker pull --platform linux/amd64 redis:8-alpine
```

先在线构建一次，导出 BuildKit 缓存。这样断网后只要 `package.json`、`pnpm-lock.yaml`、`go.mod`、`go.sum`、Dockerfile 中的 apk 安装步骤没有变化，依赖层会命中缓存：

```bash
docker buildx build \
  --platform linux/amd64 \
  --cache-to type=local,dest="$REPO/offline-cache/buildx-sub2api",mode=max \
  --load \
  -t weishaw/sub2api:latest \
  .
```

导出镜像：

```bash
docker save \
  -o "$REPO/offline-cache/images/sub2api-stack-linux-amd64.tar" \
  weishaw/sub2api:latest \
  node:24-alpine \
  golang:1.26.4-alpine \
  alpine:3.21 \
  postgres:18-alpine \
  redis:8-alpine
```

把整个仓库和 `offline-cache/` 一起带到离线机器。

## 4. 断网后：本机源码重建嵌入式前后端

先设置离线依赖缓存：

```bash
cd /path/to/sub2api
export REPO="$PWD"
export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off
export GOMODCACHE="$REPO/offline-cache/go/pkg/mod"
export GOCACHE="$REPO/offline-cache/go/build"
```

构建前端：

```bash
cd "$REPO/frontend"
pnpm install --offline --frozen-lockfile --store-dir "$REPO/offline-cache/pnpm/store"
pnpm run build
```

构建成功后，产物会写入：

```text
backend/internal/web/dist
```

再构建嵌入前端的后端：

```bash
cd "$REPO/backend"
VERSION="$(./scripts/resolve-version.sh)"
CGO_ENABLED=0 go build \
  -tags embed \
  -ldflags="-s -w -X main.Version=${VERSION}" \
  -trimpath \
  -o bin/server \
  ./cmd/server
```

如果你只想本地开发、前后端分开跑，可以不加 `-tags embed`，但浏览器要打开 Vite 的 `3000` 端口，而不是后端 `8080` 根路径。

## 5. 断网后：启动本机二进制

本机二进制需要能访问 PostgreSQL 和 Redis。它们可以是机器上已安装的服务，也可以是内网服务。

先离线生成两个固定密钥：

```bash
openssl rand -hex 32  # 用作 JWT_SECRET
openssl rand -hex 32  # 用作 TOTP_ENCRYPTION_KEY
```

首次启动推荐用环境变量自动初始化，生成 `backend/data/config.yaml`、创建数据库表和管理员账号：

```bash
cd "$REPO/backend"
mkdir -p data

DATA_DIR=./data \
AUTO_SETUP=true \
SERVER_HOST=0.0.0.0 \
SERVER_PORT=8080 \
SERVER_MODE=release \
DATABASE_HOST=127.0.0.1 \
DATABASE_PORT=5432 \
DATABASE_USER=sub2api \
DATABASE_PASSWORD='change_this_secure_password' \
DATABASE_DBNAME=sub2api \
DATABASE_SSLMODE=disable \
REDIS_HOST=127.0.0.1 \
REDIS_PORT=6379 \
REDIS_PASSWORD='' \
ADMIN_EMAIL=admin@sub2api.local \
ADMIN_PASSWORD='change_this_admin_password' \
JWT_SECRET='replace_with_64_hex_chars' \
TOTP_ENCRYPTION_KEY='replace_with_64_hex_chars' \
./bin/server
```

看到服务启动后按 `Ctrl+C` 停止。之后可以用根目录脚本后台启动：

```bash
# 确认 backend/data/config.yaml 中包含固定 TOTP 密钥。
# AUTO_SETUP 写出的 config.yaml 默认不会持久化 TOTP_ENCRYPTION_KEY；
# 如果不写入配置，后续用 start.sh 重启时会重新生成随机 key，影响已启用 2FA 的用户。
cat >> "$REPO/backend/data/config.yaml" <<'YAML'

totp:
  encryption_key: "replace_with_64_hex_chars"
YAML
```

```bash
cd "$REPO"
./start.sh
curl -fsS http://localhost:8080/health
```

常用命令：

```bash
./stop.sh
./restart.sh
tail -f backend/data/sub2api.screen.log
screen -r sub2api
```

如果你提前手写了 `config.yaml`，后端会跳过 setup wizard，不会自动创建管理员。首次部署更推荐使用上面的 `AUTO_SETUP=true`。

## 6. 断网后：Docker Compose 启动整套服务

导入镜像：

```bash
cd /path/to/sub2api
docker load -i offline-cache/images/sub2api-stack-linux-amd64.tar
docker image inspect weishaw/sub2api:latest postgres:18-alpine redis:8-alpine >/dev/null
```

如果源码有改动且依赖清单没有改动，可以断网重建应用镜像：

```bash
rm -rf offline-cache/buildx-sub2api-new
docker buildx build \
  --platform linux/amd64 \
  --network=none \
  --cache-from type=local,src=offline-cache/buildx-sub2api \
  --cache-to type=local,dest=offline-cache/buildx-sub2api-new,mode=max \
  --load \
  -t weishaw/sub2api:latest \
  .
rm -rf offline-cache/buildx-sub2api
mv offline-cache/buildx-sub2api-new offline-cache/buildx-sub2api
```

如果这一步出现 `apk add`、`pnpm install` 或 `go mod download` 缓存未命中，说明离线缓存不完整或依赖清单变了，需要回到有网环境重新准备。

创建 Docker 环境变量文件：

```bash
cd "$REPO/deploy"
cp .env.example .env
```

至少修改这些值：

```dotenv
POSTGRES_PASSWORD=change_this_secure_password
JWT_SECRET=replace_with_64_hex_chars
TOTP_ENCRYPTION_KEY=replace_with_64_hex_chars
ADMIN_EMAIL=admin@sub2api.local
ADMIN_PASSWORD=change_this_admin_password
SERVER_PORT=8080
BIND_HOST=0.0.0.0
```

密钥可以在断网机器上用 `openssl rand -hex 32` 生成。Docker Compose 会持续从 `.env` 注入 `JWT_SECRET` 和 `TOTP_ENCRYPTION_KEY`。

启动本地目录版 Compose。这个版本把数据放在 `deploy/data`、`deploy/postgres_data`、`deploy/redis_data`，迁移和备份更直接：

```bash
mkdir -p data postgres_data redis_data
docker compose -f docker-compose.local.yml up -d --pull never
docker compose -f docker-compose.local.yml ps
curl -fsS http://localhost:8080/health
```

查看日志：

```bash
docker compose -f docker-compose.local.yml logs -f sub2api
```

停止：

```bash
docker compose -f docker-compose.local.yml down
```

不要在断网环境执行：

```bash
docker compose pull
docker compose -f docker-compose.dev.yml up --build
```

前者会拉远程镜像，后者会直接走源码构建但没有配置 BuildKit 离线缓存，容易在 `apk add`、`pnpm install`、`go mod download` 处失败。

## 7. 断网后：开发模式前后端分开跑

后端：

```bash
cd "$REPO/backend"
CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server
DATA_DIR=./data ./bin/server
```

前端：

```bash
cd "$REPO/frontend"
pnpm install --offline --frozen-lockfile --store-dir "$REPO/offline-cache/pnpm/store"
VITE_DEV_PROXY_TARGET=http://localhost:8080 VITE_DEV_PORT=3000 pnpm run dev --host 0.0.0.0
```

浏览器访问：

```text
http://localhost:3000
```

Vite 会把 `/api`、`/v1`、`/setup` 代理到后端 `8080`。

## 8. 常见故障

`Frontend not embedded. Build with -tags embed to include frontend.`

说明后端不是嵌入式构建，或者 `backend/internal/web/dist/index.html` 没有在构建前生成。先跑 `pnpm run build`，再用 `go build -tags embed`。

`ERR_PNPM_NO_OFFLINE_TARBALL`

pnpm store 缺包。回到有网环境重新执行 `pnpm fetch --frozen-lockfile --store-dir ...`。

Go 仍然尝试联网或下载 toolchain。

确认目标机已安装 Go `1.26.4`，并设置：

```bash
export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off
```

Docker 离线构建失败在 `apk add`、`pnpm install` 或 `go mod download`。

BuildKit 缓存未命中。不要加 `--no-cache`，并确认离线构建时使用了 `--cache-from type=local,src=offline-cache/buildx-sub2api`。如果 Dockerfile 或依赖清单变了，必须有网重建缓存。

端口冲突。

本机脚本固定健康检查 `localhost:8080`。Docker 可以在 `deploy/.env` 修改 `SERVER_PORT`，本机二进制可以在 `config.yaml` 或 `SERVER_PORT` 中修改。

服务启动了但 AI 上游请求不可用。

离线只能保证本项目、PostgreSQL、Redis 和页面/API 服务启动。调用 OpenAI、Gemini、Claude 等外部上游仍然需要可达的外部网络或内网代理。
