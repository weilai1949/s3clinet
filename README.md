# S3 Client (s3clinet)

S3 兼容对象存储客户端工具，使用 **AWS Signature V4** 签名。提供 **Web 端** 与 **Tauri 2 桌面端**；桌面端采用 **B/S 架构**，不使用 Tauri IPC，前后端全部通过 HTTP 通信。

版本命名：稳定里程碑 **v1.0.0** 之后日常发版用**时间戳**（`v1.0.0-YYYYMMDDHHmmss`），预发布可用 **`v1.0.0-rcN`**；当前版本 `v1.0.0-rc0`；详见 [Changelog](CHANGELOG.md)。

[!TIP]
- 后端默认绑定 `127.0.0.1`（更安全），并开启 CORS 白名单与可选 Bearer 鉴权。
- 前端直传：浏览器拿到 v4 签名 URL 后**直接**上传到 S3，不经过本服务。

## 功能

- 配置账号：增删改查、连通性测试（`HeadBucket`）、列出桶（`ListBuckets`）。服务商按「兼容 / 国内 / 国外」分组：MinIO 等 S3 兼容；国内（阿里 OSS、腾讯 COS、华为 OBS、火山 TOS、百度 BOS、京东云、七牛）；国外（AWS、Cloudflare R2、Wasabi、Backblaze B2、DigitalOcean Spaces、Linode/Akamai、Scaleway、Hetzner）。
- 查看对象列表：`ListObjectsV2` 分页（「加载更多」追加），支持前缀与分隔符（目录浏览，默认 `/`）。
- 对象下载：一键生成短时效 v4 签名 GET URL 并打开；也可生成 1 小时签名 URL 复制分享。
- 前端直传：服务端生成 v4 签名 PUT URL → 浏览器直接上传（含进度，3 路并发，失败可一键重试）。
- 大文件分段上传：`≥100MB` 自动切分（10MB/段、4 路并发）直传，任一段失败即中止清理。
- 生成签名：`PresignGetObject` / `PresignPutObject` / `PresignPostObject`。
- 删除对象：`DeleteObject` / `DeleteObjects`（批量，自动 1000 分批）。
- 对象权限（ACL）：`GetObjectAcl` / `PutObjectAcl`，切换「私有 / 公共读」并复制公开访问链接。
- 对象标签（Tagging）：`GetObjectTagging` / `PutObjectTagging` / `DeleteObjectTagging`，键值行编辑，支持一键清空。
- 对象存储类型（StorageClass）：列表/详情/版本中**展示存储类型**，并支持一键切换（`STANDARD` / `STANDARD_IA` / `ONEZONE_IA` / `INTELLIGENT_TIERING` / `GLACIER` 等，`CopyObject` 副本到自身）。
- 桶属性与版本控制：查看区域 / 创建时间 / 版本控制状态，一键开启/暂停版本控制（`GetBucketLocation` / `Get/PutBucketVersioning`）；对象历史版本列表（`ListObjectVersions`，含删除标记），支持**删除指定版本 / 恢复某版本到当前**（`DeleteObject` 带 `versionId`、`CopyObject` 带 `?versionId=`）、**版本比较/详情**（选两个版本做内容差异比对）、**一键还原已删除对象**（删除标记 `DeleteObject` 撤销删除）。
- **桶管理菜单**：独立顶层菜单，集中管理各桶的**版本控制、生命周期（前缀过期删除）、服务端加密（SSE）、CORS 规则、静态网站托管、桶策略、桶标签**（读写/开关，未配置时优雅展示）。
- **回收站菜单**：独立顶层菜单，列出桶内所有**已删除对象（删除标记）**，支持**一键还原（撤销删除）**与**彻底清除（永久删除该 key 全部版本）**，可翻页加载全部历史删除标记。
- 文件迁移：同 endpoint 走 `CopyObject`（服务端复制）；跨 endpoint 走 `GetObject` → `PutObject` 流式转发（保留 Content-Type 与元数据）；逐 key 执行、失败继续并汇总。
- 复制 / 移动（跨桶）：文件与文件夹支持「复制到… / 移动到…」，统一对话框中可选目标 Bucket 与目标路径/前缀（同桶复制文件由后端 `CopyObject` 完成，文件夹递归复制；移动 = 复制成功后删除源）。
- 增加文件前缀：上传与迁移时可给对象 key 追加前缀。

## 架构（B/S + Tauri 无 IPC）

```
┌─────────────────────────────┐
│  Web 端 (浏览器)             │
│  Tauri 2 桌面壳 (B/S, 无IPC) │
└──────────────┬──────────────┘
               │ HTTP (REST /api, 可选 Bearer)
┌──────────────▼──────────────┐
│  Go 后端 (B/S Server)        │
│  · 账号配置存储              │
│  · S3 SDK v2 封装            │
│  · v4 预签名 URL 生成        │
└──────┬───────────┬─────────┘
       │           │ 前端直传
       │           ▼ (presigned v4 URL 直接 PUT 到 S3)
       │      ┌─────────────┐
       └────▶ │ S3 兼容服务  │
             │(阿里/腾讯/…)  │
             └─────────────┘
```

## 目录

```
server/      Go 后端（AWS SDK for Go v2）+ 静态托管
web/         Vue 3 + Vite + TS 前端
desktop/     Tauri 2 桌面壳（src-tauri，无 IPC）
docs/        文档（API 参考等）
docker-compose.yml   一键起 server + RustFS
```

## 环境要求

- Go 1.26+
- Node 20+ / pnpm 9+
- Rust + `@tauri-apps/cli`（仅桌面端）
- Linux 桌面端构建需 `libwebkit2gtk-4.1-dev`、`libgtk-3-dev`、`libayatana-appindicator3-dev`、`librsvg2-dev` 等

## 配置（服务端）

所有配置通过环境变量注入，支持 `.env`（见 `server/.env.example`）。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `S3C_ADDR` | `127.0.0.1:8080` | 监听地址；回环更安全，需远程改为 `0.0.0.0:8080` |
| `S3C_DATA_DIR` | `./data` | 数据目录（`accounts.json` / `accounts.db` / `accounts.json.enc`） |
| `S3C_STATIC_DIR` | `./web/dist` | Web 静态资源目录 |
| `S3C_REGION` | `us-east-1` | 账号缺省 region |
| `S3C_TOKEN` | 空 | 非空时所有 `/api/*` 需要 `Authorization: Bearer <token>`；**非回环监听时必填**（建议 `openssl rand -hex 32`）；逗号分隔支持多 token 轮换 |
| `S3C_CORS_ORIGINS` | 空 | CORS 白名单；留空=仅同源 + localhost/127.0.0.1/tauri |
| `S3C_LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `S3C_SHUTDOWN_TIMEOUT` | `30` | 收到 SIGTERM 后等待活跃连接结束的最长时间（秒） |
| `S3C_STORE_DRIVER` | `json` | 账号存储：`json` / `sqlite` / `encrypted` |
| `S3C_STORE_KEY` | 空 | `encrypted` 模式必填；Argon2id+盐派生，文件格式仅 `S3C2` |

**安全默认值**：回环绑定 + CORS 白名单 + 可选鉴权。非回环（如 `0.0.0.0`）未设 `S3C_TOKEN` 时进程**拒绝启动**。生产推荐 `docker compose -f docker-compose.prod.yml`（强制 token + encrypted，无内置 RustFS）。

## 快速开始

### 方式一：容器部署（Docker）

后端提供多阶段 `server/Dockerfile`，产出**非 root**、带健康检查、数据持久化的镜像。

```bash
# 复制环境变量（必填 S3C_TOKEN / RUSTFS_*）
cp .env.example .env
# 生成 token：openssl rand -hex 32

# 一键起 server + nginx + RustFS（会自动构建镜像）
docker compose up -d --build

# 生产（无 RustFS，强制 encrypted）
docker compose -f docker-compose.prod.yml up -d --build

# 仅运行服务端（外部 S3）
docker build -f server/Dockerfile -t s3clinet/server:v1.0.0-rc0 --build-arg GOPROXY=https://goproxy.io,direct .
docker run -d --name s3clinet -p 8080:8080 -e S3C_TOKEN="$(openssl rand -hex 32)" -v s3c-data:/data s3clinet/server:v1.0.0-rc0
```

访问：Web `http://127.0.0.1:8080`（经 **nginx**，`worker_processes 1` 反向代理 Go 后端）；RustFS 控制台 `http://127.0.0.1:9001`（凭据见 `.env` 中 `RUSTFS_*`，勿用默认口令上生产）。

TLS 终止示例见 `deploy/nginx/conf.d/s3clinet-tls.example.conf`。

**优雅重启**

```bash
# Docker：SIGTERM/SIGQUIT + stop_grace_period，不中断进行中的大文件传输
make restart-docker          # 或 ./scripts/graceful-restart.sh docker
./scripts/graceful-restart.sh nginx   # 仅 reload nginx 配置（零停机）

# 本地开发（server + web，PID 在 .run/）
make dev                     # 启动
make restart-all             # reload nginx → 重启 server → 重启 web
make dev-nginx               # Go :8081 + nginx :8080（单 worker）
make stop && make status
```

**镜像特性**
- 非 root 用户 `app` 运行；`/data` 已授权，账号数据持久化到卷。
- `HEALTHCHECK` 通过 `/s3clinet-server -healthcheck` 自检 `/api/health`。
- 配置通过 `S3C_*` 环境变量注入（见上表）；中文 `.env.example` 见 `server/.env.example`。
- 构建参数 `GOPROXY` / `NPM_REGISTRY` 可覆盖，便于国内网络。

> ⚠️ **前端直传的端点可达性**：直传使用预签名 URL，其 S3 端点必须能被**浏览器**解析。
> 若 S3 也做了容器化，账号端点请配成浏览器可达地址（例如宿主发布端口 `http://127.0.0.1:9000`，或统一域名）。
> 本 compose 中 server 用服务名 `rustfs:9000` 访问 RustFS，仅验证 server↔RustFS 服务端链路；
> 要让宿主浏览器也能直传，可给 server 增加 `network_mode: host`（Linux），并把账号端点配成 `http://127.0.0.1:9000`。

> ⚠️ **分段上传（大文件）需要 CORS 暴露 ETag**：分段直传需从每段 PUT 响应的 `ETag` 头读取指纹以完成组装，因此 Bucket 的 CORS 配置需包含 `ExposeHeader: ETag`（AWS 控制台「跨源资源共享(CORS)」或 RustFS 控制台 / S3 API 可配置）。单文件（<100MB）直传不受此限制。

### 方式二：本地

```bash
# 1) Go 后端（默认 :8080，托管 web/dist）
cd server && go run .

# 2) Web 前端（开发）
cd web && pnpm install && pnpm dev     # 代理 /api → 127.0.0.1:8080
pnpm build                            # 产物 dist/，由 Go 后端托管

# 3) 桌面端（Tauri 2）
cd desktop && pnpm install
pnpm tauri dev
pnpm tauri build
```

Web 端直接访问 `http://127.0.0.1:8080`（同源）即可。桌面端打开后前端自动把 API 基址设为 `http://127.0.0.1:8080`；后端在其他主机时可到右上角「Server」修改并保存，也可在此配置 Token。

## 测试

```bash
cd server && go test ./...        # 后端单元测试
cd web && pnpm test                 # 前端单元测试（Vitest）
cd web && pnpm typecheck          # 前端类型检查（vue-tsc）
```

一键同步版本号（自 v1.0.0 起的时间戳格式）：

```bash
./scripts/release-version.sh              # 自动生成 v1.0.0-YYYYMMDDHHmmss
./scripts/release-version.sh v1.0.0-20260902120000  # 指定时间戳版本
./scripts/release-version.sh v1.0.0-rc0             # 指定预发布版本
```

覆盖：store CRUD/持久化/脱敏/原子写/文件权限，s3wrap endpoint 归一化，handler 的 CORS 策略/鉴权/账号校验/迁移端点判断/SPA fallback/copy 部分失败（内置假 S3）。

真实 RustFS 端到端联调（`s3wrap` E2E，默认指向本地 RustFS，验证建桶/预签名直传/分段上传/复制/标签/版本控制）：

```bash
cd server && S3CLINET_E2E=1 go test ./internal/s3wrap/ -run 'TestE2E' -v
# 可选环境变量：S3CLINET_ENDPOINT / S3CLINET_ACCESS_KEY / S3CLINET_SECRET_KEY
```

CI：GitHub Actions（`.github/workflows/ci.yml`）在 push/PR 时运行 Go vet/test/build、Web typecheck/build 与 Docker 镜像构建。

## 用到的 S3 SDK for Go v2 接口

`HeadBucket`、`ListBuckets`、`CreateBucket`/`DeleteBucket`、`ListObjectsV2`、`ListObjectVersions`、`GetObject`、`HeadObject`、`PutObject`、`DeleteObject`、`DeleteObjects`、`CopyObject`、`GetBucketLocation`、`Get/PutBucketVersioning`、`Get/Put/DeleteBucketLifecycleConfiguration`、`Get/Put/DeleteBucketEncryption`、`Get/Put/DeleteBucketCors`、`Get/Put/DeleteBucketWebsite`、`Get/Put/DeleteBucketPolicy`、`Get/Put/DeleteBucketTagging`、`Get/PutObjectAcl`、`Get/Put/DeleteObjectTagging`、`CreateMultipartUpload`、`CompleteMultipartUpload`、`AbortMultipartUpload`、`PresignGetObject`、`PresignPutObject`、`PresignPostObject`、`PresignUploadPart`。

## 文档

- [REST API 参考](docs/API.md)
- [贡献指南](CONTRIBUTING.md)
- [Agent 开发规范（TDD 优先）](agents.md)
- 配置见上文矩阵。

## 安全说明

- 账号 SecretKey 仅服务端存储，对外返回**脱敏**（`******`）。
- 前端直传使用短时效 v4 签名 URL，密钥不暴露给前端。
- 默认回环绑定、CORS 白名单、可选 Bearer 鉴权；生产部署请参考上文安全建议。

## License

MIT
