# Changelog

遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。v1.0.0 之前采用 [SemVer](https://semver.org/lang/zh-CN/)；稳定里程碑后日常发版用 **`v1.0.0-YYYYMMDDHHmmss`**，预发布可用 **`v1.0.0-rcN`**（例如 `v1.0.0-rc0`）。

## [Unreleased]

## [v1.0.0-rc1] - 2026-09-02

相对 `v1.0.0-rc0`：桌面安装包由 CI 在打 tag 时交叉构建并挂到 GitHub Release。

### 工程化
- **桌面发版 CI**：`release-desktop.yml` 在 `v*` tag（或手动指定 tag）时交叉构建 NSIS `.exe` / `.deb` / `.dmg`，并上传到对应 GitHub Release。

## [v1.0.0-rc0] - 2026-09-02

首次候选发布（`develop` 全栈快照）。相对时间戳构建的主要增量：CI（gofmt / golangci-lint v2 / desktop cargo / race）打通；不做服务降级与无历史加密/账号文件格式兼容。

### 安全与可靠性
- **不做服务降级 / 无历史格式兼容**：`/api/health` store 失败返回 `503` + `status:error`；`encrypted` 仅 `S3C2`；JSON 账号文件写入 `0600`（加载时不再改旧权限）；移除 `sameEndpoint` 测试 shim 与前端「旧后端无 version」兼容注释。

### 安全与可靠性（ANALYSIS 整改·续2）
- **copyPrefix 异步**：`POST .../copy-prefix/async` 复用 job+SSE；前端文件夹复制改走异步。
- **copy-objects / delete-prefix 异步**：`POST .../copy-objects/async`、`POST .../delete-prefix/async`；多选移动与文件夹删除走 SSE。
- **多 token**：`S3C_TOKEN` 逗号分隔支持轮换。
- **防腐层**：`s3wrap.UserMessage`/`HTTPStatus`/`IsNotFound`/`ObjectStream`；handler 生产代码不再直接依赖 smithy/types；`service.CopyKeys`/`MigrateKeys`/`WriteObjectsZip`/`JobRegistry`；i18n≈570 键。
- **service 收口**：迁移引擎、ZIP、异步 Job 注册表迁出 handler；短写校验；`Entry`/`SortKey` 提到 `types.ts`。
- **可观测**：`GET /api/metrics`（Prometheus 文本）、`X-Request-ID`、可选 `S3C_LOG_JSON=1`。
- **TLS**：`docker-compose.tls.yml` 叠加 prod；a11y（aria-current/aria-sort/焦点恢复/网格键盘）；对比度与死资源清理；ANALYSIS §七整改对照。
- **长尾**：ZIP 4-worker 并行拉取；`docs/ERRORS.md`；coverage_gaps 拆分；`noUnusedLocals`；ModalDialog 组件测；i18n≈640；ServerPanel 多 token 提示；desktop tauri build 仅手动 workflow_dispatch。

### 安全与可靠性（ANALYSIS 整改·续）
- **>5GB 迁移 multipart**：跨端点流式分段；同端点 CopyObject EntityTooLarge 自动回退 multipart（`internal/service`）。
- **copyPrefix**：4 worker 并发 + `withStreamLimit`；delete-prefix 同样纳入流限。
- **加密 KDF**：`encrypted` 存储改为 magic+盐派生（现为 Argon2id / `S3C2`）。
- **API 限速**：每 IP 令牌桶约 120/min（health/静态除外）。
- **错误映射**：`writeInternalErr` 统一走 `s3HTTPStatus`（NoSuchBucket→404 等）。
- **生产**：TLS nginx 示例、README token 必填说明、`service` 包抽离流式复制。

### 前端（续）
- ObjectList 列表虚拟滚动；i18n≈308 键；ESLint 基线；token 可选 sessionStorage（`s3c_token_ephemeral=1`）。

### 安全与可靠性（ANALYSIS 整改）
- **SSRF**：S3 HTTP 客户端禁重定向 + Dial/创建时拦截链路本地与云元数据地址。
- **鉴权**：非回环监听且无 `S3C_TOKEN` 时**拒绝启动**；compose 强制 `${S3C_TOKEN:?}`。
- **RustFS 联调**：口令改为环境变量必填；CORS 收紧；镜像 pin `1.0.0-rc.3`；健康检查不依赖 curl。
- **流式写超时**：`statusRecorder.Unwrap` 修复静默失效；改为 5 分钟滚动空闲超时。
- **异步迁移**：引擎级超时/取消 API、SSE 心跳与终态可靠投递、关停取消任务；前端 EOF 回读 + 取消按钮。
- **其他**：`sameEndpoint` 保留 scheme；presign 未知 method→400、默认 1h/上限 24h；ZIP 已压缩 Store + 断开感知；SVG 排除 inline；zip-slip NTFS 消毒；5GB 用 `5e9` 字节；标签上限 10。
- **运维**：`docker-compose.prod.yml`、nginx `proxy_buffering off`、health 探测 store、SQLite `user_version`、dependabot、CI gofmt/race/cover/desktop。

### 前端
- Escape 键栈（多层弹窗只关顶层）；KeepAlive max=5；分段上传段级重试；ZIP 优先 File System Access 流式落盘；i18n 高频键扩展；回收站减少空页扫库。

### 工程化与质量
- **s3wrap 拆分**：989 行单文件拆为 `client` / `bucket` / `object` / `presign` / `multipart` / `helpers` 六文件（单文件 <400 行）。
- **错误处理收敛**：`writeInternalErr` / `writeBadJSON` / `s3UserMessage` / `batchItemError` / `s3HTTPStatus`。
- **CI**：Web `pnpm test` + lint；E2E 独立 workflow（PR 路径触发 + 每周定时）；gofmt/race/cover/golangci 门禁。
- **前端**：Hash 深链接；`upload` / `i18n` 单测；主导航中英文切换（i18n 脚手架）。
- **发版**：`scripts/release-version.sh`；`make test-all`。
- **账号存储 SQLite**：`S3C_STORE_DRIVER=sqlite`（纯 Go `modernc.org/sqlite`，无 CGO），WAL 模式 `accounts.db`。
- **账号存储加密**：`S3C_STORE_DRIVER=encrypted` + `S3C_STORE_KEY`，AES-256-GCM 静态加密 `accounts.json.enc`。
- **移除遗留迁移**：不再从明文 `accounts.json` 自动导入到 sqlite/encrypted；前端不再迁移旧版单地址 localStorage 配置。
- **异步迁移 SSE**：`POST /api/migrate/async` + `GET /api/migrate/jobs/{id}/events` 实时进度；前端迁移面板改用 SSE。

## [v1.0.0-20260901182023] - 2026-09-01

> 版本命名改为 `v1.0.0-年月日十分秒`（如 `v1.0.0-20260901182023`）。本版本整合「对象存储类型 / 版本比较 / 一键还原、桶管理、回收站」三块迭代。

### 对象存储类型 + 版本比较 + 一键还原（Batch1）
- 存储类型（StorageClass）展示与切换：`HeadObject` 返回 `storageClass`（支持 `versionId`）；对象列表/网格与「对象详情」展示；新增 `POST /api/accounts/{id}/storage-class`（`CopyObject` 副本到自身 + `x-amz-storage-class`）一键切换（标准/低频/单区/智能分层/归档等）。
- 版本比较/详情：`presign` 支持 `versionId`（`PresignGetVersion` 对历史版本生成签名 GET）；「版本」对话框新增「版本比较」——选两个内容版本并排比元数据（大小/时间/存储类型/ETag）+ 逐行内容差异高亮（二进制或 >2MB 仅比元数据，可分别下载）。
- 一键还原已删除对象：新增 `POST /api/accounts/{id}/delete-marker/restore`（`DeleteObject` 删除标记版本）撤销删除；「版本」对话框删除标记行新增「还原」。

### 桶管理菜单（Batch2）
- 新顶层菜单「桶管理」：列出/新建/删除/管理桶；桶详情含 **概览（版本控制开关）、生命周期、加密（SSE）、CORS、网站托管、桶策略、桶标签** 7 个页签。
- 后端新增 `Get/Put/DeleteBucketEncryption`、`Get/Put/DeleteBucketCors`、`Get/Put/DeleteBucketWebsite`、`Get/Put/DeleteBucketPolicy`、`Get/Put/DeleteBucketTagging`（15 个端点）；`isNoSuchBucketSetting` 把「未配置」错误映射为空响应。

### 回收站菜单（Batch3）
- 新顶层菜单「回收站」：遍历列出桶内全部删除标记（分页游标，空页自动翻页）；每项支持 **一键还原** 与 **彻底清除**（`POST /api/accounts/{id}/trash/purge`，`PurgeObject` 永删该 key 全部版本+标记）。
- 新增端点：`GET /api/accounts/{id}/trash`、`POST /api/accounts/{id}/trash/purge`。

### 测试
- 假 S3 单测：`TestChangeStorageClass` / `TestRestoreDeleteMarker` / `TestPresignGetVersion` / `TestHeadObject` / `TestListObjectVersions` / `TestBucketSettings` / `TestTrash`，覆盖各读/写/校验与未配置分支。
- 真实 MinIO E2E：`TestE2EBatch1`（删除标记还原 + 版本预签名）、`TestE2EBucketSettings`（桶策略/标签）、`TestE2ETrash`（彻底清除 3 个版本+标记、无残留）均通过（MinIO 对扩展存储类型/CORS/网站托管/SSE 的 S3 API 支持受限，已做容错说明）。

### 四轴评审反馈修复（安全 / 正确性 / 可维护性）
- **前端修复**：Vue `key` 为保留属性、不复用为 prop——7 个弹窗（ACL/HTTP头/标签/存储类型/版本/版本比较/复制移动）的对象 key 全部改为 `objectKey`，此前这些功能实际不可用（已用 Vue 3.5 SSR 复现验证）；桶管理 6 个设置页签的 `watch` 增加 `immediate`（此前永不加载、保存会用默认值覆盖真实配置）。
- **安全加固**：CORS 中间件对非白名单 Origin 的普通请求直接 403（此前仅删响应头、请求照常执行），`readJSON` 强制 `application/json`；代理 `mode=inline` 增加 MIME 白名单（恶意 HTML/SVG 强制 attachment），代理支持 `versionId` 并按版本下载、补充 `Accept-Ranges`/416；创建账号忽略客户端提交的 `id`（杜绝覆盖已有账号）；ZIP 条目名同时按 `/` 与 `\` 消毒；compose 默认仅回环发布 + 未鉴权非回环监听启动警告 + Token 过短警告；实现 `.env` 加载（自带 30 行极简解析器）。
- **正确性/健壮性**：`PurgeObject` 改 `DeleteObjects` 批量（1000/批）消除 N+1；`deletePrefix` 上限改为页前检查+跨页切分（不再超删整页）；存储类型切换映射 `InvalidRequest`→400；multipart 段号校验（1..10000 且唯一）；（copy/migrate 改为 4 路有界并发，migrate 限 10k key、failKeys 上限 200；桶属性不再静默吞错（记录日志）。
- **前端健壮性**：对象浏览/桶/回收站/迁移的列表加载增加导航序号防过期响应；上传入队时捕获所属桶（防止切换桶串桶）；迁移面板列表补传 source bucket；shift 范围选择改为从 mousedown 捕获；数据面板用 `KeepAlive` 保持状态；`catch (e: any)` 全面改为 `catch (e)` + `toErrorMessage`；ModalDialog 增加焦点陷阱与初始焦点；版本比较下载改走服务端代理（不再 `window.open` 可执行内容）。
- **测试**：handler 覆盖率 60.3% → **70.8%**，原先 0% 的主端点全部补测（列表对象/批量删除/账号 GET-PUT-DELETE/连通性/预览桶/列出桶/桶级 5 个 DELETE/CORS 拒跨域/JSON Content-Type）；CORS 拒绝行为新增断言；E2E 空标签探针改为真实断言；新增前端 vitest 测试（`versionDiff`/`format`/`storageClass`/`errors`，17 例）并提取公共 `versionDiff` 模块（修复公共后缀被标成 `context` 的语义错误）。
- **清理**：删除死代码（`s3wrap.PresignGet`、`s3api.health`/`getAccount`、`genSign`/`SignUrlDialog`）；`.gitignore` 加入根 `data/`；文档同步（API.md 补 `preview-buckets`、README SDK 接口列表补全纠错、E2E 命令改为 `-run 'TestE2E'` 全量）。
- **第二轮补漏**：AWS SDK 类型外泄清理（对象/桶/ACL 转换下沉 `s3wrap`：`FromS3Object`/`FormatBuckets`/`DescribeACL`/`GranteeLabel`，handler 不再直接依赖 `s3.ListBucketsOutput` 等）；`streamCopy` 增加 5GB 单次上传上限提示；`filename*` 改用 RFC 5987 编码；store 持久化临时文件改 `O_CREATE|O_EXCL`（防抢占/符号链接重定向）且目录 0700、掩码标记语义化（`model.IsMaskedSecret`）；PreviewOverlay 支持 Escape 关闭；对象右键菜单支持方向键导航与初始焦点；抽取 `useBucketSetting` 组合式（桶管理 6 个设置页签去重，剩各自取数/提交逻辑）；api.ts 补充 localStorage 令牌安全权衡说明。

## [20260901.2] - 2026-09-01

### 对象版本：删除指定版本 / 版本回滚
- 后端 `s3wrap` 新增 `DeleteObjectVersion`（`DeleteObject` 带 `versionId`）与 `RestoreObjectVersion`（`CopyObject` 从 `?versionId=` 复制回当前 key；版本控制下写出一条新版本，返回新 VersionId）。
- 新增端点：`DELETE /api/accounts/{id}/version`（删除指定版本）与 `POST /api/accounts/{id}/version/restore`（把某历史版本恢复为当前）。
- 「对象版本」对话框每行新增「恢复」（删除标记禁用）与「删除」操作，均带确认弹窗与结果提示，操作后自动刷新版本列表。
- 假 S3 单元测试覆盖：删除版本（传 `versionId`）、恢复版本（`X-Amz-Version-Id` 回读）、缺 key / versionId 校验 400。
- 真实 MinIO 端到端验证：恢复历史版本（`CopyObject` 带 `?versionId=`）成功并写出一条新版本、删除指定版本成功。

## [20260901] - 2026-09-01

### 桶属性 + 对象版本管理
- 后端 `s3wrap` 新增 `GetBucketLocation` / `GetBucketVersioning` / `PutBucketVersioning` / `ListObjectVersions`。
- 新增端点：`GET /bucket-info`（区域 / 创建时间 / 版本控制状态）、`PUT /bucket-versioning`（`Enabled|Suspended`）、`GET /versions`（对象各版本 + 删除标记 + 分页游标）。
- 对象浏览页位置栏新增「桶属性」：展示区域、创建时间、版本控制状态，并可一键**开启/暂停版本控制**。
- 对象右键新增「版本」：列出该对象的历史版本（最新 / 历史 / 删除标记，含 VersionId/时间/大小）。
- **真实 MinIO 端到端联调**（新增 `s3wrap` E2E 测试，`S3CLINET_E2E=1` 运行）：验证建桶、PutObject/GetObject/HeadObject、`CopyObject`、**预签名 PUT 直传**、**三段式 Multipart（预签名单段 PUT + 组装）**（12MB 组装回读一致）、对象标签、`PutBucketVersioning` + 多版本覆盖写 + `ListObjectVersions`。
- 假 S3 单元测试覆盖：桶属性（区域/创建时间/版本状态）、版本控制开关与非法状态 400、版本列表（版本 + 删除标记）。

### 代码质量（Review 驱动重构）
- **后端 handler 拆分**：`handler.go` 2186 行 → 131 行，按领域拆出 `routes.go`/`middleware.go`/`accounts.go`/`buckets.go`/`objects.go`/`copy.go`/`proxy.go`/`zip.go`/`headers.go`/`multipart.go`/`metadata.go`/`migrate.go`/`health.go`（同包，行为不变）。
- **去除冗余 `/copy` 端点**（`copyObjects`，前端未使用），保留 `copy-object` / `copy-objects` / `copy-prefix` 三件套；相应移除其单测与文档。
- **默认桶可空语义**：`BucketOrDefault()` 空时返回 `""`（不再臆造 `"default"`）；新增 `bucketOr` 助手，请求缺省桶且账号无默认桶时返回明确的 `400 bucket is required`（替代静默打到不存在的桶）。
- **`downloadZip`**：zip 条目名脱敏（路径分隔符、`..` 上跳），单次打包上限 `1000` 个对象。
- **安全强化**：新增 `Content-Security-Policy`（脚本仅同源等）；`http.Server` 增加 `ReadTimeout`（不设 `WriteTimeout` 以免截断流式大文件）；`main.go` 版本默认值同步为 `20260901`。
- **前端 ObjectsPanel 拆分**：2041 行 → **442 行的编排层**。把 11 个弹窗/覆盖层与对象列表、工具栏、上传队列、桶列表、右键菜单抽为独立 `*.vue` 子组件；再把约 900 行编排逻辑抽到 `web/src/composables/`（`useObjectBrowser` / `useObjectActions` / `usePreview`）。行为与视觉不变。
- **`proxyUrl` 去重**：抽到共享 `web/src/proxy.ts`，`ObjectsPanel` 与 `PreviewOverlay` 共用一份实现。
- **handler 测试拆分**：`handler_test.go` 1861 行 → 按领域拆成 `helpers_test.go`/`accounts_test.go`/`buckets_test.go`/`objects_test.go`/`multipart_test.go`/`metadata_test.go`/`migrate_test.go`（33 个测试函数 + 6 个助手，无重复丢失）。
- 新增 `agents.md`：TDD 优先的开发规范与验收清单（与本改动一并落地）。

## [1.0.0] - 2026-08-28

首个稳定版（v1）。整合 v0.16.0 之后的全部迭代，此后按 `v1.0.0-年月日十分秒` 版本发布。

### 复制 / 移动 / 删除（跨桶，全闭环）
- 单文件复制 `POST /copy-object`（不删源）与批量复制/移动 `POST /copy-objects`（保留文件名，`deleteSource` 移动）；文件/文件夹统一「复制到… / 移动到…」对话框（目标桶下拉 + 目标路径/前缀）。
- 移动语义：文件 = 复制成功后删除源；文件夹 = 复制成功后删除源前缀（副本有失败则保留源）。
- 修复 `rename` 跨桶同名移动被误拒（同桶内 `newKey == key` 才拒绝）。
- 未知类型文件行内「预览」按钮常显。

### 大文件分段上传（Multipart Upload 直传）
- 后端 `multipart/init | part | complete | abort` 四端点；前端 `≥100MB` 自动切 10MB 段、4 路并发直传，任一段失败自动 abort；小文件仍单 PUT。
- 要求 Bucket CORS 暴露 `ETag` 响应头（见 README）。

### 对象元数据管理（HTTP 头 / ACL / 标签）
- 对象权限 ACL：`GET/PUT /object-acl`，私有/公共读切换 + 复制公开链接。
- 对象标签 Tagging：`GET/PUT /object-tags`，键值编辑、一键清空；无标签返回空列表（兼容 `NoSuchTagSet`）。

### 导航与交互
- 左侧菜单更名（账号管理 / 文件上传 / 服务器设置等），按依赖分组「数据操作 / 配置」，首次进入按账号存在路由到「对象管理」。

### 后端与测试
- 新增 S3 接口：`CreateMultipartUpload` / `Complete/AbortMultipartUpload` / `PresignUploadPart` / `Get/PutObjectAcl` / `Get/Put/DeleteObjectTagging`。
- 假 S3 单元测试覆盖上述能力与参数校验；`go test ./...`、`pnpm build` 均通过。

## [0.16.0] - 2026-08-28

### 文件迁移优化
- **源/目标 Bucket 下拉选择**：不再手输，加载账号下全部桶（含默认桶选项）。
- **目标账号默认值**：打开面板自动预选（优先另一个账号，其次同账号=复制到其他桶/前缀），避免忘选；下拉标注「（同账号）」。
- **迁移进度**：分批执行（每批 50 个）并实时显示「迁移中 x/y」与进度条。
- **迁移结果弹窗**：成功/失败/总计统计 + **失败对象清单**（后端新增 `failedKeys` 字段，最多展示 200 个，可滚动）+ 首个错误详情 + 「**去目标账号查看**」一键跳转（切换账号并进入对象管理）。
- **已选统计**：显示已选文件数与合计大小。
- 真实 MinIO 端到端验证：同账号迁移（CopyObject 路径）成功、目标桶结构正确、清理干净；`failedKeys` 字段经真实失败场景验证。

## [0.15.0] - 2026-08-28

### 行内「⋯ 更多操作」（互联网表格习惯）
- 对象列表每行右侧新增**常显「⋯」按钮**：点击弹出与右键一致的完整菜单（文件：下载/预览/复制签名链接/复制 Key/重命名/详情/删除；文件夹：打开/复制路径/复制/移动/删除文件夹），锚定按钮下方右对齐。
- 文件行操作列精简为「下载 / 预览 + ⋯」：签名、详情等低频操作统一收入更多菜单，行更干净（其他按钮仍 hover 显示，⋯ 常显）。
- 文件夹行操作列不再空白，同样提供 ⋯ 入口。
- CDP 真实浏览器验证：⋯ 弹出菜单、菜单项完整、菜单内操作（详情）联动正常。

## [0.14.0] - 2026-08-28

### 查看对象（显式化，云控制台习惯）
- **行操作新增「预览」按钮**：hover 行即可看到「下载 / 预览 / 签名 / 详情」，预览入口不再深藏右键菜单。
- **双击 = 查看**：双击文件打开预览弹窗（图片/视频/音频/PDF/文本/代码；未知类型自动转下载），双击文件夹进入——与 OSS/COS 控制台一致。
- **Enter = 查看选中文件**：快捷键语义从「下载」改为「查看」，未知类型自动转下载；快捷键提示条文案同步。
- 预览面板本身不变（v0.9.0 起的安全代理预览，文本转义渲染、PDF 沙箱）。

## [0.13.0] - 2026-08-28

### 对象属性与生命周期（参考 OSS/COS/TOS 控制台共性）
- **编辑对象 HTTP 头**：详情弹窗「编辑 HTTP 头」——修改 Content-Type 与自定义元数据（键值行编辑、可增删）；后端 `CopyObject` 复制到自己并 `MetadataDirective: REPLACE`，保存后详情即时刷新。
- **生命周期规则**：Bucket 列表每行「生命周期」——规则管理弹窗（规则 ID/前缀/过期天数，增删后整体保存）；后端 `Get/PutBucketLifecycleConfiguration`（简化版前缀过期删除）。
  - 未配置规则正确返回空列表（兼容 `NoSuchLifecycleConfiguration` 错误码）；
  - 清空规则走 `DeleteBucketLifecycle`（空 PUT 会被 MinIO 等实现拒绝，真实环境验证后修正）。
- 假 S3 单元测试：REPLACE 指令与 Content-Type/元数据头、生命周期读写 XML、非法规则（天数/重复 ID）校验；真实 MinIO 端到端验证（set-headers 后 head 确认、规则保存/读取/清空）。

## [0.12.0] - 2026-08-28

### 控制台化：Bucket 管理（参考 OSS/COS/TOS 网页控制台共性）
- **Bucket 列表页**：对象管理首层展示 Bucket 表格（名称/创建时间/进入/删除），点击「进入」管理对象；有默认桶的账号自动进入默认桶。
- **创建 Bucket**：弹窗表单（名称 + 读写权限：私有/公共读/公共读写），后端 `CreateBucket`（自动附带地域 LocationConstraint）；创建后自动进入。
- **删除 Bucket**：确认框提示须先清空；桶非空时后端返回 409 语义化提示。
- **统计条**：对象页顶部显示当前目录对象数/总大小、选中项数与合计大小（控制台习惯）。
- **列表 / 网格视图切换**：网格卡片（类型图标/名称/大小），单击选中、双击打开、右键菜单与列表一致。
- **返回桶列表**：对象页操作栏「← 返回桶列表」。
- 后端：`POST /api/accounts/{id}/bucket`、`DELETE /api/accounts/{id}/bucket`（含假 S3 测试：创建路径/ACL 头/命名校验/删除/非空 409）+ 真实 MinIO 端到端验证（创建/列表/删除/非法名 400）。

## [0.11.0] - 2026-08-28

### UI 交互：点击内容统一弹窗化
- 新增通用 **ModalDialog** 组件（标题栏 + 关闭按钮 + Esc/遮罩关闭 + 内容滚动）。
- **对象详情**、**签名 URL 结果**：由列表下方内嵌面板改为弹窗，不再被滚动带走，查看/复制更聚焦。
- **账号新增/编辑表单**、**服务端新增/编辑表单**：由内嵌表单改为弹窗（模态操作更清晰，主流 SaaS 习惯）。
- 保留内嵌的：上传进度（需持续可见）、迁移结果（消息条足够）、Toast、确认/输入/预览弹窗（已是弹窗）。

## [0.10.0] - 2026-08-28

### UI 交互重新设计
- **对象管理工具条重构**：拆分为「位置栏」（Bucket + 面包屑 + 路径编辑 + 过滤）与「操作栏」（刷新/上级 | 上传/新建文件夹 | 全选/批量操作），分组分隔线，主操作更突出。
- **文件管理器式行交互**：单击行=切换选中（文件夹=进入），双击=打开，操作按钮（下载/签名/详情）hover 时显示（窄屏常显），行内新增「详情」入口。
- **键盘快捷键**：`Enter` 下载选中文件、`F2` 重命名、`Delete` 删除所选、`Ctrl/Cmd+A` 全选（输入框/按钮聚焦时不抢占）；首次展示可关闭的快捷键提示条（localStorage 记忆）。
- **账号快速切换**：对象管理面板头部「当前账号」下拉，直接切换账号无需回账号面板。
- **统一 busy 态**：递归复制/移动/删除文件夹等耗时操作加防重复提交，相关按钮联动禁用。
- **空状态引导**：无账号时提供「+ 创建第一个账号」按钮，一键跳转账号面板并自动打开新增表单。
- **错误提示可重试**：对象管理错误条附「重试」按钮。
- **跨面板联动**：前端直传完成 Toast 带「查看对象」动作按钮，一键跳转对象管理；Toast 组件支持动作按钮。
- 上传入口更名为「上传文件」，操作栏按钮文案精简。

## [0.9.0] - 2026-08-28

### 新增
- **常见格式安全预览**：对象管理右键「预览」按类型分发——图片（含 SVG，`<img>` 上下文脚本不执行）、视频 / 音频（原生播放器，代理支持 Range 拖动）、PDF（`sandbox` iframe 禁脚本）、文本与代码（txt/md/json/csv/log/源码等 50+ 扩展名，转义文本展示，超大文件截断提示）；未知格式提示下载。

### 安全加固（展示侧攻击面收敛）
- **预览 / 下载统一走服务端代理**（`GET /api/accounts/{id}/proxy`）：
  - `mode=download` 强制 `Content-Disposition: attachment`，恶意 HTML/SVG/JS 内容**永不进入浏览器渲染管道**；
  - `mode=text` 强制 `text/plain + nosniff` 并服务端截断（默认 1MB），前端以转义文本渲染，根除 HTML 注入 / XSS；
  - 文件名清洗（去路径分隔符 / 引号 / 控制字符），防 `Content-Disposition` 头注入。
- 移除预览面板「在新窗口打开」；签名 URL 面板提示勿在浏览器直接打开（复制分享场景保留）。
- 对象管理所有「下载」入口（行内按钮 / 右键 / 双击）改为代理下载。
- 文本内容全程 `{{ }}` 转义展示，不渲染任何 HTML/Markdown。

### 后端
- 新增 `GET /api/accounts/{id}/proxy`（download/inline/text 三模式，Range 透传，404/400 语义化）。
- 假 S3 单元测试：attachment/inline 头、Content-Type 透传、Range 转发、文本强制纯文本与截断标记、nosniff、404/400、文件名清洗；真实 MinIO 端到端验证。

## [0.8.0] - 2026-08-28

### 新增
- **上传到当前目录**：对象管理工具栏直接选择文件上传（复用 presign PUT 直传，2 路并发），内嵌进度条与逐文件状态，完成后自动刷新列表；无需再切换到「前端直传」面板。
- **面包屑地址栏可编辑**：点击 ✎ 将路径变为输入框，直接输入完整前缀回车跳转（Esc 取消），深目录直达。
- **图片预览**：右键图片文件「预览」，弹层内嵌大图展示（缩放自适应），可一键在新窗口打开原图。
- **Shift 范围多选**：按住 Shift 点击复选框，连续选中区间内所有文件（文件管理器习惯）。

### 修复与改进
- 对象管理面板工具栏布局调整：上传/刷新/上级目录分组，右侧主操作（新建文件夹）保持。
- 预览、上传进度等新组件样式与深色模式自动适配。

## [0.7.0] - 2026-08-28

### 新增
- **国内主流对象存储预设**：登录页服务商增加腾讯云 COS、华为云 OBS、火山引擎 TOS、百度智能云 BOS、京东云 OSS、七牛云 Kodo；选区域自动填充 Endpoint / 公网 Endpoint（S3 兼容域名）。
- **海外常见对象存储预设**：AWS S3、Cloudflare R2、Wasabi、Backblaze B2、DigitalOcean Spaces、Linode/Akamai、Scaleway、Hetzner。
- **服务商三行分组**：兼容 / 国内 / 国外，便于快速选择。
- **批量下载（ZIP 打包）**：对象管理选中多个文件一键打包下载；后端 `POST /download-zip` 流式打包（不落盘、不占内存），获取失败的对象写入包内 `_下载失败清单.txt`。
- **删除文件夹（递归）**：右键文件夹「删除文件夹（含全部内容）」，后端循环 `ListObjectsV2` + 批量删除，上限 10 万对象保护；空前缀拒绝（防误删全桶）。
- **复制 / 移动文件夹（递归）**：右键输入目标前缀，后端逐 key `CopyObject`（同桶目标前缀与源重叠时拒绝，防无限复制）；移动 = 全部复制成功后才删除源。
- 真实 MinIO 端到端验证：复制结构正确、ZIP 条目正确、递归删除干净。

### 后端
- 新增 `POST /api/accounts/{id}/delete-prefix`、`POST /api/accounts/{id}/copy-prefix`、`POST /api/accounts/{id}/download-zip` 三个端点。
- 假 S3 单元测试：ZIP 内容与失败清单、递归删除分页计数、递归复制计数与重叠前缀拒绝、跨桶同前缀放行。

### 修复与改进
- 前端 `requestBlob` 支持二进制响应（错误时仍解析 JSON error）。
- 对象管理工具栏新增「下载所选(ZIP)」（打包中有 loading 态）。

## [0.6.0] - 2026-08-28

### 新增
- **对象列表「加载全部」**：循环分页直到末尾（上限 200 页保护），完成后汇总提示已加载的文件/文件夹数。
- **迁移面板「列出全部文件」**：列出源前缀下**所有文件（含子目录）**，循环分页（单页 1000、上限 20 万对象），支持全选一键迁移；已用真实 MinIO 验证递归与单层列出的差异。
- **上传完成项「复制链接」**：上传完成后一键复制 1 小时签名下载链接，即传即分享。
- 剪贴板能力抽取为共享模块 `web/src/clipboard.ts`（Clipboard API + textarea 降级），对象面板与上传面板统一使用。

### 修复与改进
- 对象列表与迁移面板的批量加载均有页数上限保护，避免超大桶长时间卡死；超出上限时明确提示已加载数量。
- 迁移面板「列出全部」与「列出对象」互斥禁用，防止并发请求交错。

## [0.5.0] - 2026-08-28

### 新增
- **对象管理增强**：
  - **新建文件夹**：工具栏一键创建（服务端 PUT 空对象，key 自动补全 `/` 结尾），真实 S3 验证通过。
  - **重命名 / 移动**：右键菜单操作，输入新 Key（可含路径即移动）；后端先 `CopyObject` 成功后才删除源，复制失败不丢数据；支持跨桶移动（`newBucket`）。
  - **对象详情**：右键「详情」调 `HeadObject`，展示 Key / 大小 / 修改时间 / Content-Type / ETag / 元数据；对象不存在返回 404。
  - **批量复制签名链接**：选中多个文件一键复制 1 小时签名链接（每行一个）。
- 通用输入对话框 `PromptDialog`（自动聚焦全选、Enter 确认、Esc 取消、内置校验），与确认框视觉统一。

### 后端
- 新增 `GET /api/accounts/{id}/head`（HeadObject 详情，404 语义化）、`POST /api/accounts/{id}/mkdir`（空对象建目录）、`POST /api/accounts/{id}/rename`（copy+delete）。
- 假 S3 单元测试：head 详情字段与 404/400、mkdir 路径规范化与空 body、rename 先复制后删除及复制失败不删源、同 key 拒绝。

### 修复与改进
- 前端交互文案与空状态保持一致；详情面板与签名面板同风格（深色模式自动适配）。

## [0.4.0] - 2026-08-28

### 新增
- **深色模式**：支持「跟随系统 / 浅色 / 深色」三态循环切换（顶栏主题按钮），选择持久化；全部颜色 token 化，深色下表格、表单、弹层、Toast、骨架屏等一并适配。
- **自定义确认对话框**：替换原生 `confirm()`（删除账号/对象/服务端统一体验），支持 Esc 取消、Enter 确认、遮罩点击关闭，危险操作红色警示图标。
- **对象管理右键菜单**：文件 → 下载 / 复制签名链接（1 小时）/ 复制 Key / 删除；文件夹 → 打开 / 复制路径。
- **对象管理双击交互**（文件管理器习惯）：双击文件=下载，双击文件夹=进入。
- **对象列表列排序**：名称 / 大小 / 修改时间表头可点击排序（升/降序切换），文件夹恒置顶。
- **对象列表本地即时过滤**：输入关键字过滤当前目录已加载条目，显示命中数，一键清除。
- **记住上次选中的账号**：刷新 / 重开后自动恢复上次登录的账号。
- 上传面板新增「清除已完成」：只移除已完成项，保留等待 / 失败项便于重试。

### 修复与改进
- 顶栏新增主题切换按钮（☀️ / 🌙 图标随实际主题变化，系统主题变化时自动刷新）。
- 表格可排序表头带 hover 反馈与排序指示箭头。
- 全局按钮 / 输入框 / 表格 / 面板等硬编码颜色全部收敛为 CSS 变量，为深色主题与后续定制提供统一入口。
- 对象管理空状态区分「无对象」与「无匹配项」两种提示。

## [0.3.0] - 2026-08-28

### 新增
- **OSS 式登录**（参考阿里云 OSS Browser）：账号表单新增「服务商」选择（阿里云 OSS / AWS S3 / S3 兼容）+「区域」预设下拉（OSS 21 个地域、AWS 20 个区域，选中自动填充 Endpoint 与公网 Endpoint），保存后自动切换为当前账号；新增 `web/src/regions.ts` 预设数据。
- 对象管理：新增「下载」操作（短时效 presign GET 直接打开）；「加载更多」改为追加分页并显示已加载数量。
- 前端直传：3 路并发上传 + 「重试失败」一键重传。
- CI：新增 GitHub Actions 工作流（Go vet/test/build、Web typecheck/build、Docker 镜像构建）。
- Web：`pnpm typecheck`（vue-tsc），`pnpm build` 前置类型检查。
- Go 测试：SPA fallback、鉴权路径边界、JSON 尾部数据、copy 部分失败（假 S3）、账号文件权限。
- **`/api/health` 返回服务端版本号**（`version` 字段，ldflags 注入）；「服务端配置」连通性检测后展示后端版本标签。
- 所有响应（含静态资源）新增基础安全头：`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: no-referrer`。

### UI 改版
- **C 端视觉风格**：浅色网格渐变背景、玻璃拟态顶栏（backdrop-blur）、品牌渐变 Logo 与渐变标题、侧边栏激活项渐变胶囊、浮动卡片（大圆角 + 柔和投影）、渐变主按钮/进度条、标签页切换过渡动画、卡片入场动效、细圆角滚动条。
- 设计系统重写：语义化 CSS token、统一间距/圆角/阴影、按钮尺寸体系（`btn sm`）、表格粘性表头与选中行高亮、`focus-visible` 焦点环。
- 头部重构：服务器地址/Token 收进「⚙ 设置」弹层；新增后端连接状态指示灯（已连接/连接异常）。
- 全局 Toast 反馈（右下角、自动消失）：账号增删改、对象删除、复制、上传完成等操作结果。
- 对象管理：面包屑导航（根目录 / 逐级可点）、骨架屏加载、目录名改为可键盘聚焦的按钮、空状态带图标。
- 前端直传：拖拽上传区（点击/拖拽/键盘均可触发）、整体进度条、单文件移除。
- 账号表单：编辑时 SecretKey 占位提示「留空则保持不变」；连通性状态简化为 未测试/测试中/正常/失败。
- 迁移面板：Bucket 输入框占位显示账号默认桶；大小列格式化。
- 响应式：窄屏（<900px）侧边栏折叠为顶部横向导航；`index.html` 增加 favicon 与 theme-color。
- 主题色：品牌渐变与主色调由科技蓝切换为**薄荷绿**（`--brand` `#2dbd98→#0e8c66`、`--primary` `#10a37c`），并联动阴影、焦点环与各 tint。

### 修复与改进
- **修复跨 endpoint 流式迁移在明文 HTTP 端点失败**：AWS SDK for Go v2 1.107 默认对 `PutObject` 计算请求 CRC32 校验与 payload SHA-256，而迁移的流式 body 不可 seek，非 TLS 端点直接报错（`unseekable stream is not supported...`）。现改为 `RequestChecksumCalculation=WhenRequired`，并通过 finalize 中间件在 payload hash 计算前预置 `UNSIGNED-PAYLOAD`（与 HTTPS 下 SDK 行为一致），流式迁移恢复正常（新增假 S3 集成测试覆盖）。
- **修复 SPA fallback 失效**：静态文件未命中时此前直接 404，现对无扩展名的页面路由正确回退 `index.html`；带扩展名的缺失资源仍 404。
- **安全**：Bearer Token 校验改为常量时间比较（SHA-256 摘要 + `subtle.ConstantTimeCompare`），降低时序侧信道风险；`accounts.json` 写入权限 `0600`（含明文密钥）；`Update` 持久化失败回滚内存状态。
- **健壮性**：S3 HTTP client 增加建连/TLS/响应头超时与连接池上限（不设整体超时，避免截断大文件流式迁移）。
- 前端「下载」由 `window.open` 改为程序化锚点点击，避免异步请求后被浏览器弹窗拦截（新标签页无法打开）。
- `/api/accounts/{id}/copy` 与 migrate 行为对齐：逐 key 复制、失败继续，响应新增 `failed`/`lastError` 字段。
- 鉴权路径判断精确化（`/api` 或 `/api/` 前缀），不再误伤 `/apiary` 之类路径；JSON 请求体拒绝尾部多余数据；健康检查日志降为 debug。
- 日志状态记录器忽略多余的 `WriteHeader` 调用，避免状态与实际响应不一致。
- 前端：对象列表分隔符默认 `/`（与占位提示一致，目录可浏览）；全选 checkbox 状态修正；迁移列表大小格式化；GET 请求不再携带多余 `Content-Type`（避免跨域预检）。
- 重构：新增 `web/src/format.ts` 共享格式化工具（`fmtSize`/`fmtDate`），对象/直传/迁移三个面板去重。
- 构建：Dockerfile 拷贝 `pnpm-lock.yaml` 并改 `--frozen-lockfile`（可复现）；Go 构建通过 ldflags 注入版本号；Makefile 补全 `.PHONY` 并新增 `test`/`vet`/`web-typecheck`/`docker` 目标；移除 pnpm 11 已忽略的 `package.json#pnpm` 字段。
- 测试：新增 config 包测试（默认值/覆盖/列表解析）、健康检查版本与安全头断言、错误 Token（含长度不同）鉴权用例、migrate 跨 endpoint 流式复制假 S3 集成测试。

## [0.2.0] - 2026-08-22

### 新增
- 服务端集中配置（`internal/config`），支持 `S3C_*` 环境变量与 `.env`（见 `server/.env.example`）。
- **安全加固**：默认绑定回环 `127.0.0.1`；CORS 白名单（`S3C_CORS_ORIGINS`）；可选 Bearer 鉴权（`S3C_TOKEN`）。
- **容器化**：多阶段 `server/Dockerfile`（web + Go + `debian:bookworm-slim` 运行时，非 root `app`，自带 `HEALTHCHECK`，`/data` 已授权）；`docker-compose.yml`（server + MinIO）；构建参数 `GOPROXY`/`NPM_REGISTRY` 可覆盖；新增 `/s3clinet-server -healthcheck` 自检子命令。
- Go 单元测试：store / s3wrap / handler（CRUD、持久化、脱敏、原子写、CORS 策略、鉴权、迁移端点判断）。
- 文档：REST API 参考（`docs/API.md`）、配置矩阵、运行/部署说明、容器直传可达性说明。

### 修复与改进
- 修复跨 provider 迁移误用 `CopyObject`：`sameEndpoint` 仅在两端点一致且非空时判定为同端点，否则流式复制。
- 跨 endpoint 迁移保留源对象 `Content-Type` 与元数据。
- 预签名有效期限制在 1s–7 天；对象 `maxKeys` 限制 1–1000。
- 账号更新增加必填校验，避免清空 endpoint/accessKey。
- 账号存储改为原子写（临时文件 + rename）；`Create` 写盘失败回滚内存状态。
- 账号缺省 region 为空时回退 `us-east-1`。
- 前端：上传队列直接持有 `File` 引用（避免同名误匹配）；设置区新增 Token；签名复制支持降级；对象列表加载状态。

## [0.1.0] - 2026-08-22

### 新增
- 首个版本：Go 后端（AWS SDK for Go v2，封装 11 个 S3 接口）、Vue3+Vite+TS 前端（账号/列对象/直传/签名/删除/迁移/加前缀）、Tauri 2 桌面壳（无 IPC，B/S）。
