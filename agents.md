# Agents — 开发规范（TDD 优先）

本文件面向在本仓库工作的 AI 代理（agent）与人类贡献者，约定**测试驱动开发（TDD）**的落地方式。所有改动必须可验证、可回退、不破坏既有绿灯。

## 核心原则

1. **先写会失败的测试，再写让它通过的实现。**
2. **小步提交**：一个逻辑改动一个 commit；改动前后都要 `go test ./...` 与 `pnpm build` 全绿。
3. **行为驱动，不测内部实现**：断言「外部可见的行为 / 返回值 / HTTP 状态码」，不要断言私有函数或内部变量。
4. **红灯-绿灯-重构（红绿蓝）**：先看到测试因缺实现而失败（红），再让实现通过（绿），最后在测试保护下优化结构（重构）。

## 三类测试（按项目现有基建落地）

| 层 | 位置 | 运行方式 | 目的 |
|----|------|----------|------|
| **单元 / 行为测试** | `server/internal/.../*_test.go` | `cd server && go test ./...` | 用 `httptest.NewServer` 的**假 S3** 验证 handler 层逻辑（路由、参数校验、正/反例、错误码映射）。 |
| **真实对端 E2E** | `server/internal/s3wrap/e2e_test.go` | `S3CLINET_E2E=1 go test ./internal/s3wrap/ -run 'TestE2E' -v` | 验证最硬核路径：**SigV4 签名 / 预签名直传 / 分段 Multipart 组装 / 跨 bucket 复制 / 标签 / 版本控制**。默认指向本地 RustFS。 |
| **前端类型 + 构建** | `web` | `cd web && pnpm build`（含 `vue-tsc --noEmit`） | 类型安全与可构建性；UI 改动同时保留手测/截图证据。 |

### 假 S3 模式（handler 测试）
- 用 `httptest.NewServer` 模拟 S3，采用 **path-style**（请求落在 `/{bucket}/{key}`）。
- 依 `r.URL.Query().Has("acl"|"tagging"|"versions"|"location"|"versioning")` 与 HTTP 方法分发返回的 XML。
- 返回标准 S3 XML（`<ListVersionsResult>`、`<AccessControlPolicy>`、`<Tagging>` 等），并按需返回特定错误码（如 `NoSuchTagSet`）验证容错。

### 真实 RustFS 联调
- 需要时用 `docker compose up -d rustfs`（默认 `rustfsadmin/rustfsadmin`，S3 API 9000、控制台 9001）。
- E2E 测试用 `S3CLINET_E2E=1` 门控，普通 `go test ./...` 不会执行，CI 因此不受影响。

## 必验门禁（每次改动提交前）

```bash
cd server && go vet ./... && go test ./...   # 后端
cd server && go build ./...                   # 后端可构建
cd web && pnpm test && pnpm build             # 前端单测 + 类型检查 + 构建
# 涉及签名/直传/分段/复制/标签/版本时，额外跑真实 RustFS E2E
cd server && S3CLINET_E2E=1 go test ./internal/s3wrap/ -run 'TestE2E' -v
# 或 make test-all（后端 + 前端单测）
```

## 验收清单（按 review 五轴）

提交前逐项核对并在 PR 描述中说明：

- **正确性**：符合需求；边界（空值/空列表/截断/大文件）覆盖；错误路径有测试；测试断言的是行为不是实现。
- **可读性**：命名贴近领域且与项目一致；控制流平直；无死代码 / 无 rest 兼容 shim。
- **架构**：沿用 `store → model → s3wrap → handler` 分层；`AWS` 类型不外泄到 handler/前端；**单文件不超过约 1000 行**，超过先拆再改；Feature 逻辑不进共享模块。
- **账号存储**：`S3C_STORE_DRIVER` 支持 `json`（默认）/ `sqlite` / `encrypted`；`store.Open` 统一入口。
- **安全**：用户输入在边界校验；敏感字段（`SecretKey`）不落地 localStorage、不写日志；输出转义（禁 `v-html`）；外部数据视为不可信。
- **性能**：列表端点分页；分段上传有界并发；无 N+1 / 无界循环。
- **验证**：测试绿 + 构建绿 + 涉及 UI 的保留截图/手测记录。

## 提交规范

- 用 [Conventional Commits](https://www.conventionalcommits.org/zh-CN/)：`feat:` / `fix:` / `refactor:` / `test:` / `docs:`。
- 每个 commit 只做一件事；**重构与功能分开**。
- 版本：自稳定里程碑 v1.0.0 起用 **`v1.0.0-年月日十分秒`**（`v1.0.0-YYYYMMDDHHmmss`，例如 `v1.0.0-20260901182023`）。改版本号时运行 `./scripts/release-version.sh`（或手动同步 `CHANGELOG.md`、`docs/API.md`、`Makefile`、`package.json`、`Cargo.toml`、`tauri.conf.json`、`docker-compose.yml`、`README.md`）。
  - **展示形式**（带 `v` 前缀）：`Makefile` 的 `VERSION`、Docker 镜像 tag、`README.md` / `docs/API.md` / `CHANGELOG.md` 中的版本号、`server/main.go` 的缺省 `version`、`server/Dockerfile` 的 `ARG VERSION`。
  - **机器形式**（合法 SemVer，去掉 `v` 前缀）：`web/package.json`、`desktop/package.json`、`desktop/src-tauri/Cargo.toml`、`desktop/src-tauri/tauri.conf.json` 的 `version` 字段（如 `1.0.0-20260901182023`）。
- 不提交构建产物（`dist/`、`node_modules/`、`target/`）。

## Red Flags（遇到即停下修正）

- 没看到红灯就直接写实现；
- 大而全的单文件改动（>1000 行）；
- 用 `any`/断言私有成员「掩盖」不清晰的不变量；
- 一次升级一批依赖 / 手改 lockfile；
- 功能逻辑渗入共享工具模块；
- 「以后再说」的清理不会发生——提交前就清干净。
