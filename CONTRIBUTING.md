# Contributing

感谢你的贡献！本仓库包含 Go 后端、Vue 前端与 Tauri 桌面壳。

## 分支与提交流程

采用 `develop`（主开发）→ `main`（稳定发布）工作流：

```bash
git checkout develop
git pull
git checkout -b feat/<你的功能>
# ... 开发 ...
pnpm build           # web
cd server && go test ./...
go build ./...
git commit -m "feat: ..."
```

## 开发环境

- Go 1.26+
- Node 20+ / pnpm 9+
- Rust + `@tauri-apps/cli`（桌面端）

后端本地启动（如需连本机 RustFS）：

```bash
# 推荐：与 compose 一致的 RustFS 单容器
docker compose up -d rustfs

# 或手动启动
docker run -d --name s3clinet-rustfs -p 127.0.0.1:9000:9000 -p 127.0.0.1:9001:9001 \
  -e RUSTFS_VOLUMES=/data -e RUSTFS_ADDRESS=0.0.0.0:9000 \
  -e RUSTFS_CONSOLE_ADDRESS=0.0.0.0:9001 -e RUSTFS_CONSOLE_ENABLE=true \
  -e RUSTFS_ACCESS_KEY=rustfsadmin -e RUSTFS_SECRET_KEY=rustfsadmin \
  -e RUSTFS_CORS_ALLOWED_ORIGINS='*' \
  rustfs/rustfs:latest

cd server && go run .
cd web && pnpm dev
```

账号 Endpoint 示例：`http://127.0.0.1:9000`，AccessKey/SecretKey：`rustfsadmin` / `rustfsadmin`，勾选 Path-style。

## 提交规范

- 使用 [Conventional Commits](https://www.conventionalcommits.org/zh-CN/)：`feat:`、`fix:`、`docs:`、`refactor:`、`test:` 等。
- 改动前先跑 `go test ./...`、`go vet ./...`、`pnpm build`。
