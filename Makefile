# 版本号：日常发版 v1.0.0-YYYYMMDDHHmmss；预发布可用 v1.0.0-rcN。
# 手动覆盖构建：make VERSION=v1.0.0-rc0 server-build
VERSION ?= v1.0.0-rc0

.PHONY: server server-build web web-build web-typecheck desktop-dev desktop-build test web-test test-all vet docker all dev dev-nginx restart restart-server restart-web restart-nginx restart-docker restart-all stop status

# Go 后端（构建并运行）
server:
	cd server && go mod tidy && go build -ldflags="-X main.version=$(VERSION)" -o s3clinet-server . && ./s3clinet-server

# 构建 Go 二进制（注入版本号）
server-build:
	cd server && go build -ldflags="-X main.version=$(VERSION)" -o s3clinet-server .

# Web 前端开发
web:
	cd web && pnpm install && pnpm dev

# 构建 web 产物（含类型检查）
web-build:
	cd web && pnpm install && pnpm build

# 前端类型检查
web-typecheck:
	cd web && pnpm install && pnpm typecheck

# 桌面端开发（Tauri）
desktop-dev:
	cd desktop && pnpm install && pnpm tauri dev

# 打包桌面端
desktop-build:
	cd web && pnpm install && pnpm build
	cd desktop && pnpm install && pnpm tauri build

# 后端测试
test:
	cd server && go test ./...

# 前端单元测试
web-test:
	cd web && pnpm install && pnpm test

# 后端 + 前端单测
test-all: test web-test

# 后端静态检查
vet:
	cd server && go vet ./...

# 构建 Docker 镜像（注入版本号）
docker:
	docker build -f server/Dockerfile -t s3clinet/server:$(VERSION) --build-arg VERSION=$(VERSION) .

# 一键构建全部
all: server-build web-build

# ---- 本地开发 & 优雅重启 ----
dev:
	bash scripts/run-dev.sh

dev-nginx:
	bash scripts/run-dev.sh --nginx

restart-server:
	bash scripts/graceful-restart.sh server

restart-web:
	bash scripts/graceful-restart.sh web

restart-nginx:
	bash scripts/graceful-restart.sh nginx

restart-docker:
	bash scripts/graceful-restart.sh docker

restart-all:
	bash scripts/graceful-restart.sh all

stop:
	bash scripts/graceful-restart.sh stop

status:
	bash scripts/graceful-restart.sh status
