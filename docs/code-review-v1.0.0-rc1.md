# s3clinet 全方位代码评估报告（v1.0.0-rc1）

- 评估日期：2025-09-03
- 评估基线：commit `f5a9ef1`（release: prepare v1.0.0-rc1）
- 评估方式：实测质量门禁 + 三路独立深度审查（后端 Go / 前端 Vue·TS / 桌面·部署·CI）+ 人工交叉仲裁
- 性质：只读审查，未改动代码；子代理关键指控逐条人工复核 8 处，其中 1 处被证伪（见 §4）

## 1. 验证基线（本机实测）

| 门禁 | 结果 |
|---|---|
| `go vet` / `go build` / `go test ./...` | ✅ 全绿 |
| `go test -race`（竞态检测） | ✅ 无数据竞争 |
| `gofmt -l` | ✅ 零未格式化 |
| 前端 vitest 34 单测 / `vue-tsc` / `vite build` / ESLint | ✅ 全绿（333KB JS / 101KB gzip） |
| 文档一致性 | ✅ docs/API.md 与 routes.go 67/67 端点逐条一致；版本号 4 处一致 |
| git 卫生 | ✅ 无二进制/数据/密钥被跟踪（含历史）；无硬编码 AKIA/私钥 |
| 单测覆盖率 | ⚠️ handler 67.5%、config 87.2%、**s3wrap 22.4%**、service 41.2%、store 63.8%、model 70% |

## 2. 总评：7.5 / 10

分层纪律（`store → model → s3wrap → handler`）执行到位、安全默认值成体系、文档零漂移、
依赖纪律极佳（前端 dependencies 仅 `vue`）。无 Critical，无未认证方可利用漏洞。

| 五轴 | 后端 | 前端 | 桌面/部署/CI |
|---|---|---|---|
| 正确性 | 7（进度编排缺陷） | 6.5（3 处功能缺陷） | 8 |
| 可读性 | 7.5 | 7.5（i18n 超长） | 8 |
| 架构 | 7（分层部分失守） | 7（双上传队列） | 8 |
| 安全 | 8.5 | 8.5 | 8 |
| 性能 | 7.5 | 7 | 7.5 |

## 3. Required（按杠杆排序；✅=已人工复核证实）

### 后端 server/
1. ✅ **异步任务进度全程停在 0/total** — `service/batch.go:69`、`service/migrate.go:60`、`handler/copy.go:261`
   三处 worker-pool 先 `wg.Wait()` 再排空 results，onProgress/Emit 只在结束时一次性触发。
   copy-objects/async、copy-prefix/async、migrate/async 的 SSE 与轮询进度形同虚设。
   修法：单独 goroutine 边收 results 边回调（`handler/objects.go:339` deletePrefixAsync 为正确范本）；
   补「进行中进度>0」行为测试。
2. ✅ **持久化永久失败隐患** — `store/store.go:180`、`store/encrypted.go:229`
   `O_WRONLY|O_CREATE|O_EXCL` 写 `path+".tmp"`，进程在写后 rename 前崩溃留下 tmp 残骸，
   此后每次保存账号都失败。修法：persist 前先 `os.Remove(tmp)`（保留 O_EXCL 防抢占语义）。
3. ✅ **SSRF 漏拦 AWS IMDS IPv6** — `s3wrap/ssrf.go:61-75`
   `isBlockedIP` 未拦 `fd00:ec2::254`（unique-local 非 link-local），两级校验均放行。
   修法：加精确黑名单（不宜全拦 `fd00::/8`，误伤自托管 IPv6 主场景）。
4. ✅ **分层失守** — s3wrap 直接返回 SDK 类型进 handler/service：`handler/objects.go:32`、
   `metadata.go:276,104`、`service/stream_copy.go:22,36`，违反 agents.md「AWS 类型不外泄」。
   修法：比照 `s3wrap/s3wrap_dto.go` 的 ObjectItem 模式补齐 versions/tags/get-object DTO。
5. ✅ **SQLite List 失败静默返回空** — `store/sqlite.go:131-135` 查询失败只打 stderr 返回 nil，
   API 返回 200+空列表，用户误以为账号全丢。修法：`AccountStore.List` 增加 error 返回。

### 前端 web/src/
6. ✅ **上传「中止即重启」** — `composables/useObjectActions.ts:315,360-366`
   onDeactivated 调 abortAllUploads 后，worker 把 AbortError 置回 `status='pending'`，
   外层 `for(;;)` 立即重新组批：切后台后上传从 0 重启并在后台继续，unmount 后亦然。
   修法：引入 `cancelled` 终态并跳过；给 `UploadQueue.vue` 补取消按钮（abortUploadItem 已实现无 UI 入口）。
7. **全局快捷键跨面板误触** — `composables/useObjectBrowser.ts:289-314,449-466`
   ObjectsPanel 被 KeepAlive 缓存，切走后 window keydown 监听仍存活。
   修法：`onGlobalKey` 开头加 `if (!panelActive.value) return`。
8. **回收站加载竞态** — `components/RecycleBinPanel.vue:48-101`
   快速切桶时两个 loadMarkers 循环并发，旧桶数据拼进新桶列表。仿 useObjectBrowser 的 loadSeq 守卫。
9. **SSE 进度逐条 toast** — `composables/useObjectActions.ts:461` + `components/DestDialog.vue:151`
   数千对象的大前缀删除会灌满 toast 栈（store.ts:58 每条 3.6s）。改节流或单条就地更新。
10. **20 万行无虚拟滚动** — `components/MigratePanel.vue:105,342-352`
    listAll 上限 200×1000 直接 `<tr v-for>` 渲染会冻结页面。复用 ObjectList.vue:56-92 窗口化方案。
11. **两套并行上传队列** — `components/UploadPanel.vue:86-110` vs `useObjectActions.ts:338-382`
    并发数（3 vs 2）、条目结构、状态机各写一份且已漂移。抽共享 `useUploadQueue`。
12. **i18n/index.ts 1350 行** — 超仓库 ~1000 行规范。按域拆 zh/en 模块，index.ts 只留 t/tf/locale。

### 桌面/部署/CI
13. **GitHub Actions 未 pin SHA** — 含 `contents: write` 的第三方 `tauri-apps/tauri-action@v1`
    （release-desktop.yml:20,101）与浮动 `@stable`（ci.yml:127,148）。修法：全部 pin SHA。
14. **发布产物无校验和/签名** — release-desktop.yml:100-121 仅上传产物，无 SHA256SUMS。
15. ✅ **Tauri IPC 面大于「无 IPC」宣称** — `tauri.conf.json:13` `withGlobalTauri: true` +
    capabilities `core:default`；前端仅用 `isTauri()`（api.ts，另有 UA/hostname 信号）做探测。
    修法：关 `withGlobalTauri` 并裁剪 capability。
16. ✅ **PID 复用误杀** — `scripts/lib/process.sh`：is_running 仅 `kill -0`，PID 复用会误杀无关进程。
    修法：kill 前校验 `/proc/$pid/cmdline` 含预期进程名。

## 4. 被仲裁否决的指控（诚实记录）

「GET /api/accounts 回传明文 SecretKey」**不成立**：`listAccounts`/`getAccount` 响应路径全部走
`Sanitized()`（store.go:73、sqlite List、encrypted.go:124 三驱动一致；accounts.go:51 亦显式脱敏）；
明文 SecretKey 仅存在于服务端签名内部使用的 `Get()`，从未写回响应。
**降级建议保留为 Optional**：契约里 `secretKey` 恒为 `"******"` 占位，可改 `secretSet: boolean`。

## 5. Optional（摘要）

- 后端：encrypted 驱动 Update 失败不回滚内存（与 json 驱动不一致，store.go:135-138 有回滚）；
  流式复制 64MB/worker 缓冲建议 sync.Pool；`/api/metrics` 无鉴权暴露运行时指标；
  migrate SSE 无写超时；worker-pool 模式仓内重复 4 份可收敛泛型 helper；
  保留 ProxyFromEnvironment 会让 HTTP(S)_PROXY 环境绕过 SSRF 拨号校验（建议 S3 transport `Proxy=nil`）；
  deleteObjects 未限 key 数上限；用户 metadata 未校验以 500 而非 400 返回。
- 前端：showDetail 无 seq 守卫；SSE 订阅未在 onBeforeUnmount abort；VersionsDialog 忽略分页截断；
  copySelectedLinks 串行 presign N+1；面板挂载隐式切全局账号；`no-explicit-any:'off'` 可收紧。
- 部署/CI：pnpm 版本口径不一（9 vs 11）；README 示例建议绑回环；nginx 容器缺内存限制；
  `|| true` 吞安装失败；Makefile 每次启动 `go mod tidy` 易漂移。

## 6. Nit（摘要）

后端：X-Request-ID 回显可塞超长值；Bearer scheme 大小写敏感（RFC 7235 应不区分）；
`metadata.go:40` 变量遮蔽外层 `r *http.Request`；.env 按进程 CWD 相对加载；UserMessage 靠
字符串匹配 "exceeds 5GB"；validBucketName 未限首字符/结尾点连字符；`ssrf.go:94-99` To4 分支两支
相同（死代码）。
前端：`s3api.downloadZip`/`requestBlob` 死代码（api.ts:499-500,245-248）；同批 files 排序两次
（useObjectBrowser.ts:94-113）；13 处 `ctx.account.value!.id` 非空断言；blob 下载同步
revokeObjectURL；`{} as KeyBindings` cast hack。

## 7. 亮点（值得保持）

1. **安全纵深成体系**：SSRF 双重校验（创建时+拨号时防 DNS rebinding）+ 全局禁重定向；
   CORS 对白名单外 Origin 直接 403 阻断简单请求 + readJSON 强制 application/json 触发预检，双层防 CSRF；
   CSP 收紧 script-src 'self'。
2. **SecretKey 全链路纪律**：三驱动统一 `Sanitized()` 出口、错误信息脱敏映射、掩码回写保护、
   加密驱动 Argon2id + AES-256-GCM + 原子写 + 0600。
3. **流式纪律严格**：proxy/zip/迁移全程 `io.Copy` 零整对象缓冲，withStreamLimit(32) +
   滚动写超时 + statusRecorder.Unwrap 穿透。
4. **前端竞态防护有意识**：loadSeq 序号守卫、AbortController+指针双重新鲜度检查；
   预览管线（三模式代理+类型拒渲染+sandbox）完整；无障碍投入真实（焦点陷阱/aria-live）。
5. **文档与实现零漂移** + 版本号自动同步脚本；CI 有 job 级权限、并发取消、覆盖率守卫；
   部署默认即安全（回环发布、token 强制注入、非 root、healthcheck）。
6. **依赖纪律极佳**：前端 dependencies 仅 `vue`；后端仅 AWS SDK + uuid + x/crypto + 纯 Go sqlite。

## 8. 行动顺序（本次评估后的整改计划）

1. 进度回调编排（三处同款修法，收敛成正确 worker-pool helper，顺带解决 Optional 重复问题）
2. persist tmp 清理 与 fd00:ec2::254 黑名单
3. 前端一次整改：`cancelled` 终态 + panelActive 守卫 + 回收站 seq 守卫
4. CI 一次整改：全部 actions pin SHA + 发布加 SHA256SUMS
5. i18n 拆分与双上传队列收敛
6. 贯穿：消除死代码；先补齐单测覆盖率再动刀
## 9. 整改落地记录（v1.0.0-rc1 评估后）

按第 8 节行动顺序执行完毕，全部提交对应 `go vet/build/test -race` 与 `pnpm test/build` 门禁：

| # | 事项 | 提交 |
|---|---|---|
| 1 | s3wrap fake-S3 单元测试补齐（覆盖率 22.4%→61.5%） | `5c2ff52` |
| 2 | R1 `RunBatch[I]` 泛型池收敛（batch.go）+ 进度竞态修复 | `2cae3f9` |
| 3 | R2 persistLocked 残留 tmp 清理（O_EXCL 前置 os.Remove） | `abef0fb` |
| 4 | R3 SSRF 阻断表补 IMDS IPv6 `fd00:ec2::254` | `be8d390` |
| 5 | CI/桌面/脚本加固（actions SHA 锁定、SHA256SUMS、Tauri 权限收敛、PID 校验、回环端口） | `4e6c0f0` |
| 6 | R5 Store.List 错误传播（json/encrypted/sqlite） | `4b303a8` |
| 7 | 前端四项：上传中止即重启（cancelled 终态 + toRaw 键归一化）、面板快捷键误触（panelActive 守卫）、回收站 loadSeq 竞态、SSE 进度 toast 节流 | `0356b8d` |
| 8 | s3wrap 覆盖率 61.5%→87.0%（object 全生命周期 fake 测试） | `00089e3` |
| 9 | R4 防腐层：s3wrap 全部返回 DTO，handler/service 零 SDK 类型；presign 零过期守卫（93.4%） | `fc3269c` |
| 10 | i18n 按域拆分（1352→82 行）+ 双上传队列收敛为共享状态机 | `88e0205` |
| 11 | 死代码清理：api.ts requestBlob/downloadZip、useObjectBrowser 冗余文件排序；后端扫描无未引用导出 | 本次提交 |

注：第 4 节「GET /api/accounts 返回明文 SecretKey」一项为误报（所有出口均 `Sanitized()`），
未采纳；见第 4 节原文。
