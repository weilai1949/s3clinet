# REST API 参考

后端默认监听 `127.0.0.1:8080`。所有 `/api/*` 响应均为 JSON（`/api/metrics` 除外）。若设置 `S3C_TOKEN`，除 `/api/health` 与 `/api/metrics` 外，所有请求需携带 `Authorization: Bearer <token>`。
所有响应带 `X-Request-ID`（客户端可传入，否则服务端生成）；访问日志字段 `req` 与之对应。

错误格式：`{"error": "..."}`  
通用码：`400`（请求错误）、`401`（未鉴权）、`404`（未找到）、`500`（服务端错误）。  
S3 错误码与用户消息对照见 [`docs/ERRORS.md`](./ERRORS.md)。

## 健康检查

```
GET /api/health
```
```json
200 {"status":"ok","version":"v1.0.0-rc1","time":"...","store":{"ok":true}}
```
`version` 为服务端版本号（构建时经 ldflags 注入），可用来核对前后端版本是否匹配。store 探测失败时返回 `503` + `"status":"error"`（不做降级；不健康即失败）。

## 指标

```
GET /api/metrics
```
Prometheus 文本格式（无需鉴权）。含 HTTP 计数、uptime、goroutine、内存与 `s3c_build_info`。

## 账号

### 列出
```
GET /api/accounts
```
```json
200 {"accounts":[{"id":"...","name":"...","endpoint":"...","secretKey":"******","...":true}]}
```
`secretKey` 始终脱敏为 `******`。

### 创建
```
POST /api/accounts
```
```json
{"name":"minio","endpoint":"http://localhost:9000","region":"us-east-1","accessKey":"ak","secretKey":"sk","bucket":"b","pathStyle":true,"useSSL":false}
```
必填：`name`、`endpoint`、`accessKey`、`secretKey`。`pathStyle` 用于 MinIO/OSS 等第三方。

### 获取
```
GET /api/accounts/{id}
```

### 更新
```
PUT /api/accounts/{id}
```
字段同创建；`secretKey` 传空或不传则保留原值（不会把 `******` 回写）。

### 删除
```
DELETE /api/accounts/{id}
```

### 预览桶（不落库）
```
POST /api/accounts/preview-buckets
```
```json
{"name":"N(可选)","endpoint":"http://minio:9000","region":"us-east-1","accessKey":"ak","secretKey":"sk","pathStyle":true}
```
用表单凭证临时列出桶（不保存账号），用于新建账号时选择默认桶。
```json
200 {"buckets":[{"name":"b1","creationDate":"..."}]}
```
缺 endpoint/accessKey/secretKey 返回 400。

### 连通性测试
```
POST /api/accounts/{id}/test
```
```json
200 {"ok":true,"bucket":"b"}
```
`ok:false` 时附 `error` 字段（HTTP 仍 200，便于前端展示）。

### 列出桶
```
GET /api/accounts/{id}/buckets
```
```json
200 {"buckets":[{"name":"b","creationDate":"..."}]}
```

### 创建桶
```
POST /api/accounts/{id}/bucket
```
```json
{"name":"new-bucket","region":"cn-north-1(可选)","acl":"private|public-read|public-read-write(可选)"}
```
名称须 3-63 位小写字母/数字/连字符/点；`region` 非 `us-east-1` 时附带 `LocationConstraint`（OSS/COS/TOS 需要）。
```json
200 {"created":"new-bucket","region":"cn-north-1","acl":"private"}
```

### 删除桶
```
DELETE /api/accounts/{id}/bucket?name=new-bucket
```
桶内须为空；非空时返回 `409 {"error":"bucket not empty, delete all objects first"}`。
```json
200 {"deleted":"new-bucket"}
```

## 对象

### 列出对象
```
GET /api/accounts/{id}/objects?bucket=B&prefix=P&delimiter=/&maxKeys=1000&continuationToken=CT
```
- `bucket` 缺省用账号默认桶；`maxKeys` 范围 1–1000（默认 1000）。
- `delimiter=/` 时用 `commonPrefixes` 返回目录。
- `continuationToken` 用于分页。
```json
200 {
  "objects":[{"key":"a.txt","size":17,"lastModified":"...","etag":"\"...\"","storageClass":"STANDARD","isDir":false}],
  "commonPrefixes":["dir/"],
  "isTruncated":false,
  "nextToken":""
}
```
`objects` 各项含 `storageClass`（存储类型），可用于列表展示。

### 对象详情
```
GET /api/accounts/{id}/head?bucket=B&key=K&versionId=V(可选)
```
`bucket` 缺省用账号默认桶；对象不存在返回 404。`versionId` 可选，指定读取某历史版本的详情。
```json
200 {"key":"dir/a.txt","size":17,"lastModified":"...","etag":"\"...\"","contentType":"text/plain","storageClass":"STANDARD_IA","metadata":{"owner":"alice"}}
```

### 新建文件夹
```
POST /api/accounts/{id}/mkdir
```
```json
{"bucket":"B(可选)","key":"images"}
```
S3 无真实目录：服务端 PUT 空对象，`key` 自动补全为以 `/` 结尾（`images/`）。
```json
200 {"created":"images/","bucket":"B"}
```

### 重命名 / 移动
```
POST /api/accounts/{id}/rename
```
```json
{"bucket":"B(可选)","key":"a.txt","newKey":"dir/b.txt","newBucket":"B2(可选)"}
```
先 `CopyObject` 到新 key，成功后才删除源（复制失败不丢数据）；`newBucket` 缺省同桶；同桶内 `newKey` 与 `key` 相同拒绝，跨桶同名移动允许。
```json
200 {"renamed":"dir/b.txt"}
```

### 复制对象（单文件，跨桶）
```
POST /api/accounts/{id}/copy-object
```
```json
{"bucket":"B(可选)","key":"a.txt","newKey":"archive/a.txt","newBucket":"B2(可选)"}
```
复制单个对象到目标桶/目标 key，**不删除源**（区别于 `rename`）；`newBucket` 缺省同桶；同桶内 `newKey` 与 `key` 相同则拒绝。
```json
200 {"copied":"archive/a.txt","bucket":"B2"}
```

### 批量复制 / 移动（所选文件）
```
POST /api/accounts/{id}/copy-objects
```
```json
{"bucket":"B(可选)","targetBucket":"B2(可选)","targetPrefix":"archive/","keys":["dir/a.txt","dir/b.txt"],"deleteSource":false}
```
把多个文件复制到目标桶 + 前缀（`targetPrefix + 文件名`，保留文件名）；`targetPrefix` 留空 = 目标桶根目录；`deleteSource=true` 时复制成功后再删除源（移动）。逐个处理、失败不中断。
```json
200 {"copied":1,"failed":1,"lastError":"(失败时才有)","failedKeys":["dir/b.txt"]}
```

```
POST /api/accounts/{id}/copy-objects/async
```
请求体与 `copy-objects` 相同。立即返回 `jobId`，进度通过既有迁移任务接口查询（`progress.migrated` = 已复制/移动成功数）。
```json
202 {"jobId":"uuid","total":2}
```

### 删除文件夹（递归）
```
POST /api/accounts/{id}/delete-prefix
```
```json
{"bucket":"B(可选)","prefix":"dir/"}
```
循环 `ListObjectsV2` + 批量 `DeleteObjects` 删除前缀下全部对象；`prefix` 必填（空前缀拒绝，防误删全桶）；上限 10 万个对象。
```json
200 {"deleted":12,"truncated":false}
```

```
POST /api/accounts/{id}/delete-prefix/async
```
请求体与 `delete-prefix` 相同。先列举再异步批量删除；`progress.migrated` 表示已删除数。
```json
202 {"jobId":"uuid","total":12,"truncated":false}
```

### 复制文件夹（递归）
```
POST /api/accounts/{id}/copy-prefix
```
```json
{"bucket":"B","prefix":"dir/","targetBucket":"B2(可选)","targetPrefix":"archive/"}
```
同账号内逐 key `CopyObject`，`targetPrefix` 直接前置到相对 key；同桶目标前缀与源重叠时拒绝（防无限复制）。
```json
200 {"copied":12,"failed":0,"total":12,"lastError":"(失败时才有)"}
```

### 设置对象 HTTP 头
```
POST /api/accounts/{id}/set-headers
```
```json
{"bucket":"B(可选)","key":"a.txt","contentType":"text/markdown","metadata":{"owner":"alice"}}
```
`CopyObject` 复制到自己并 `MetadataDirective: REPLACE`；`contentType` 留空不修改，`metadata` 整体覆盖（传空对象则清空）。
```json
200 {"updated":"a.txt"}
```

### 对象权限（ACL）
```
GET /api/accounts/{id}/object-acl?bucket=B(可选)&key=K
```
返回所有者、是否公有、授权列表与公开链接（`url` 按账号 `publicEndpoint`/path-style/useSSL 构造）。
```json
200 {"bucket":"B","key":"a.txt","owner":"owner","public":true,"grants":[{"grantee":"所有用户 (AllUsers)","permission":"READ"}],"url":"http://host/B/a.txt"}
```
```
PUT /api/accounts/{id}/object-acl
```
```json
{"bucket":"B(可选)","key":"a.txt","acl":"public-read"}
```
`acl` 取值：`private`（默认）、`public-read`、`public-read-write`、`authenticated-read`、`aws-exec-read`。
```json
200 {"acl":"public-read"}
```

### 对象标签（Tagging）
```
GET /api/accounts/{id}/object-tags?bucket=B(可选)&key=K
```
无标签时（S3 返回 `NoSuchTagSet` 等）视为空列表。
```json
200 {"tags":[{"key":"env","value":"prod"}]}
```
```
PUT /api/accounts/{id}/object-tags
```
```json
{"bucket":"B(可选)","key":"K","tags":[{"key":"env","value":"prod"}]}
```
覆盖写入；`tags` 传空数组则删除全部标签；`key` 非空且不可重复。
```json
200 {"tags":[{"key":"env","value":"prod"}]}
```

### 桶属性（区域 / 创建时间 / 版本控制）
```
GET /api/accounts/{id}/bucket-info?bucket=B(可选)
```
```json
200 {"bucket":"B","region":"cn-north-1","createdAt":"2026-09-01T00:00:00Z","versioning":"Enabled"}
```
`region` 为 `GetBucketLocation` 结果（空视为 `us-east-1`）；`versioning` 取值 `""`（未配置）/ `Enabled` / `Suspended`。

### 桶版本控制开关
```
PUT /api/accounts/{id}/bucket-versioning
```
```json
{"bucket":"B(可选)","status":"Enabled"}
```
`status` 取值 `Enabled` 或 `Suspended`。
```json
200 {"versioning":"Enabled"}
```

### 桶服务端加密（SSE）
```
GET /api/accounts/{id}/bucket/encryption?bucket=B
```
```json
200 {"bucket":"B","configured":true,"algorithm":"AES256","kmsKeyId":"","bucketKeyEnabled":true}
```
未配置时返回 `configured:false`。

```
PUT /api/accounts/{id}/bucket/encryption
```
```json
{"bucket":"B(可选)","algorithm":"AES256","kmsKeyId":"(可选)","bucketKeyEnabled":true}
```
`algorithm` 取值 `AES256` / `aws:kms` / `aws:kms:dsse`。
```
DELETE /api/accounts/{id}/bucket/encryption?bucket=B
200 {"deleted":"B"}
```

### 桶 CORS 规则
```
GET /api/accounts/{id}/bucket/cors?bucket=B
200 {"bucket":"B","rules":[{"id":"r1","allowedMethods":["GET"],"allowedOrigins":["*"],"allowedHeaders":["*"],"exposeHeaders":["ETag"],"maxAgeSeconds":3600}]}
```
```
PUT /api/accounts/{id}/bucket/cors
{"bucket":"B(可选)","rules":[{...}]}
```
`rules` 传空数组时删除全部规则。
```
DELETE /api/accounts/{id}/bucket/cors?bucket=B
200 {"deleted":"B"}
```

### 桶静态网站托管
```
GET /api/accounts/{id}/bucket/website?bucket=B
200 {"bucket":"B","configured":true,"indexDocument":"index.html","errorDocument":"error.html","redirectAllRequestsTo":""}
```
```
PUT /api/accounts/{id}/bucket/website
{"bucket":"B(可选)","indexDocument":"index.html","errorDocument":"error.html","redirectAllRequestsTo":""}
```
`indexDocument` 或 `redirectAllRequestsTo` 至少填其一。
```
DELETE /api/accounts/{id}/bucket/website?bucket=B
200 {"deleted":"B"}
```

### 桶策略
```
GET /api/accounts/{id}/bucket/policy?bucket=B
200 {"bucket":"B","configured":true,"policy":"{...}"}
```
```
PUT /api/accounts/{id}/bucket/policy
{"bucket":"B(可选)","policy":"{\"Version\":\"2012-10-17\",...}"}
```
`policy` 必须是合法 JSON；传空字符串时删除桶策略。
```
DELETE /api/accounts/{id}/bucket/policy?bucket=B
200 {"deleted":"B"}
```

### 桶标签
```
GET /api/accounts/{id}/bucket/tags?bucket=B
200 {"bucket":"B","tags":[{"key":"env","value":"prod"}]}
```
```
PUT /api/accounts/{id}/bucket/tags
{"bucket":"B(可选)","tags":[{"key":"env","value":"prod"}]}
```
`tags` 传空数组时删除全部标签。
```
DELETE /api/accounts/{id}/bucket/tags?bucket=B
200 {"deleted":"B"}
```

### 对象版本列表（ListObjectVersions）
```
GET /api/accounts/{id}/versions?bucket=B(可选)&prefix=P&keyMarker=K&versionIdMarker=V
```
```json
200 {
  "versions":[{"key":"a.txt","versionId":"v2","isLatest":true,"lastModified":"2026-09-01T01:00:00Z","size":2,"etag":"\"e2\"","storageClass":"STANDARD"}],
  "deleteMarkers":[{"key":"a.txt","versionId":"dv1","isLatest":true,"lastModified":"2026-09-01T00:00:00Z"}],
  "isTruncated":false,"nextKeyMarker":"","nextVersionIdMarker":""
}
```
需桶版本控制开启后才保留多版本；`isLatest` 标记当前版本，`deleteMarkers` 为删除标记，`storageClass` 为该版本的存储类型。

### 删除指定版本
```
DELETE /api/accounts/{id}/version?bucket=B(可选)&key=K&versionId=V
```
`versionId` 必填；可删除普通版本或删除标记（版本控制下删除对象不传 versionId 只会新增删除标记）。
```json
200 {"deleted":"K","versionId":"V"}
```

### 版本回滚（恢复某版本为当前）
```
POST /api/accounts/{id}/version/restore
```
```json
{"bucket":"B(可选)","key":"K","versionId":"V"}
```
把该版本复制回当前 key（`CopyObject` 带 `?versionId=`）；版本控制下会写出一条新的当前版本，`versionId` 为新版本号。
```json
200 {"restored":"K","versionId":"<新版本号>"}
```

### 一键还原已删除对象（恢复删除标记）
```
POST /api/accounts/{id}/delete-marker/restore
```
```json
{"bucket":"B(可选)","key":"K","versionId":"V"}
```
`versionId` 必须是删除标记的版本号。S3 语义：删除标记是一个无数据版本，删除（`DeleteObject` 带 `versionId`）该标记即完成「撤销删除」，对象回到被删除前的状态。
```json
200 {"restored":"K","versionId":"V"}
```

### 切换对象存储类型（StorageClass）
```
POST /api/accounts/{id}/storage-class
```
```json
{"bucket":"B(可选)","key":"K","versionId":"V(可选)","storageClass":"STANDARD_IA"}
```
通过 `CopyObject` 副本写入自身并携带新 `x-amz-storage-class`（`versionId` 为空切换当前对象，否则切换指定版本）。版本控制桶下写出一条新版本；`storageClass` 支持 `STANDARD` / `STANDARD_IA` / `ONEZONE_IA` / `INTELLIGENT_TIERING` / `GLACIER` / `GLACIER_IR` / `DEEP_ARCHIVE` / `REDUCED_REDUNDANCY` / `EXPRESS_ONEZONE`。
```json
200 {"changed":"K","versionId":"<新版本号>","storageClass":"STANDARD_IA"}
```

### 回收站（列出删除标记）
```
GET /api/accounts/{id}/trash?bucket=B(可选)&prefix=P&keyMarker=K&versionIdMarker=V&maxKeys=1000
```
返回桶内删除标记（已删除对象），带分页游标（ListObjectVersions 过滤为删除标记）。逐页加载时若某页无删除标记，调用方继续用游标向后翻页即可。
```json
200 {
  "deleteMarkers":[{"key":"a.txt","versionId":"dv1","isLatest":true,"lastModified":"2026-09-01T00:00:00Z"}],
  "isTruncated":false,"nextKeyMarker":"","nextVersionIdMarker":""
}
```

### 彻底清除（永久删除对象）
```
POST /api/accounts/{id}/trash/purge
```
```json
{"bucket":"B(可选)","key":"K"}
```
删除该 key 的全部版本与删除标记（不可再还原）；非版本控制桶兜底删除当前对象。
```json
200 {"purged":"K","deleted":3}
```

### 生命周期规则（前缀过期删除）
```
GET /api/accounts/{id}/lifecycle?bucket=B
```
```json
200 {"rules":[{"id":"r1","prefix":"logs/","days":30}]}
```
未配置规则返回空列表。基于 S3 兼容生命周期 API（MinIO/AWS 支持，部分厂商兼容性有限）。

```
PUT /api/accounts/{id}/lifecycle
```
```json
{"bucket":"B","rules":[{"id":"r1","prefix":"logs/","days":30}]}
```
`rules` 传空数组时删除全部规则（`DeleteBucketLifecycle`，空 PUT 会被多数实现拒绝）。每条规则需 `id` 唯一且 `days >= 1`。
```json
200 {"updated":1}
```

### 批量下载（ZIP 打包）
```
POST /api/accounts/{id}/download-zip
```
```json
{"bucket":"B(可选)","keys":["a.txt","dir/b.txt"]}
```
服务端流式打包 ZIP（不落盘），响应 `Content-Type: application/zip`；获取失败的对象写入包内 `_下载失败清单.txt`。

### 安全代理（下载 / 预览）
```
GET /api/accounts/{id}/proxy?bucket=B&key=K&mode=download|inline|text&maxBytes=N
```
统一走服务端转发，避免签名 URL 暴露与恶意内容渲染：

- `mode=download`（默认）：强制 `Content-Disposition: attachment` 流式转发（浏览器直接保存，**内容不进渲染管道**），支持 `Range` 请求头透传。
- `mode=inline`：透传源 `Content-Type` 流式转发（图片 / PDF / 媒体预览），支持 `Range`。
- `mode=text`：读取前 `maxBytes` 字节（默认 1MB，上限 2MB），**强制** `text/plain; charset=utf-8` + `X-Content-Type-Options: nosniff`，超限时响应头 `X-Preview-Truncated: 1`（杜绝 HTML/JS 注入）。

对象不存在返回 404；文件名经清洗（去路径分隔符/引号）后写入 `Content-Disposition`，防止头注入。

### 生成签名
```
POST /api/accounts/{id}/presign
```
```json
{"method":"get|put|post","key":"dir/a.txt","bucket":"B(可选)","versionId":"V(可选,仅get)","expiresIn":3600}
```
- `expiresIn` 单位秒，范围 1s–7 天（S3 上限），默认 15 分钟。
- `get` / `put` 返回 `{method,url,expiresIn,...}`；`post` 额外返回 `{url,fields}`（multipart 表单字段）。
- `get` 可传 `versionId` 生成指向指定历史版本的签名 GET（用于「版本比较/详情」拉取某个版本内容）。
```json
get: {"method":"get","bucket":"B","key":"k","url":"https://...X-Amz-Signature=...","expiresIn":3600}
post: {"method":"post","bucket":"B","key":"k","url":"https://...","fields":{"X-Amz-Signature":"...","key":"k"},...}
```

### 分段上传（大文件直传）
用于大文件（前端 `≥100MB` 自动使用；<100MB 走单 PUT）。四步：
```
POST /api/accounts/{id}/multipart/init
{"bucket":"B(可选)","key":"big.bin","contentType":"application/octet-stream(可选)"}
200 {"uploadId":"UPLOAD123","key":"big.bin","bucket":"B"}
```
```
POST /api/accounts/{id}/multipart/part
{"bucket":"B(可选)","key":"big.bin","uploadId":"UPLOAD123","partNumber":1,"expiresIn":3600}
200 {"partNumber":1,"url":"https://...X-Amz-Signature=...","expiresIn":3600}
```
浏览器 PUT 到 `url` 后需读取响应头 `ETag`（要求 Bucket CORS 暴露 `ETag`）。
```
POST /api/accounts/{id}/multipart/complete
{"bucket":"B(可选)","key":"big.bin","uploadId":"UPLOAD123","parts":[{"partNumber":1,"etag":"\"e1\""}]}
200 {"completed":"big.bin"}
```
```
POST /api/accounts/{id}/multipart/abort
{"bucket":"B(可选)","key":"big.bin","uploadId":"UPLOAD123"}
200 {"aborted":true}
```
`partNumber` 范围 1–10000；`parts` 需按段号对应各自 `etag`；失败时应调用 `abort` 清理。

### 删除对象（批量）
```
POST /api/accounts/{id}/delete
```
```json
{"bucket":"B(可选)","keys":["a.txt","b.txt"]}
```
SDK 单次最多 1000，服务端自动分批。
```json
200 {"deleted":2}
```

### 跨账号迁移
```
POST /api/migrate
```
```json
{
  "sourceAccountId":"a","sourceBucket":"B","sourceKeys":["x.txt"],
  "targetAccountId":"b","targetBucket":"B2","targetPrefix":"migrated/"
}
```
- 源、目标 endpoint 一致时用 `CopyObject`（服务端复制）；否则 `GetObject`→`PutObject` 流式转发（保留 Content-Type/元数据）。
- 逐个对象迁移，任一失败继续其余；失败对象 key 通过 `failedKeys` 返回（便于前端展示失败清单）。
```json
200 {"migrated":1,"failed":1,"lastError":"(失败时才有)","failedKeys":["bad.txt"]}
```

#### 异步迁移（SSE 进度）
```
POST /api/migrate/async
```
请求体与 `POST /api/migrate` 相同。
```json
202 {"jobId":"uuid","total":100}
```

```
GET /api/migrate/jobs/{id}
```
```json
200 {"jobId":"uuid","done":true,"progress":{"done":100,"total":100,"migrated":98,"failed":2,"status":"done"},"result":{"migrated":98,"failed":2,"failedKeys":["..."]}}
```

```
POST /api/accounts/{id}/copy-prefix/async
```
请求体与 `copy-prefix` 相同。立即返回 `jobId`，进度通过既有迁移任务接口查询：
`GET /api/migrate/jobs/{id}` / `.../events` / `POST .../cancel`（`progress.migrated` 表示已复制数）。

```
GET /api/migrate/jobs/{id}/events
```
`Content-Type: text/event-stream`。每行 `data: {"done":1,"total":100,...}`，结束时 `status:"done"` 或 `status:"cancelled"`；另有 `event: ping` 心跳。

```
POST /api/migrate/jobs/{id}/cancel
```
取消运行中的异步迁移/复制任务（已完成则 `cancelled:false`）。
```json
200 {"jobId":"uuid","cancelled":true}
```

## 静态资源

- 非 `/api` 路径由 Go 托管 `web/dist`；未命中的页面路由（无扩展名）回退到 `index.html`（SPA），带扩展名的缺失资源返回 404。
- 所有响应（含静态资源）携带基础安全头：`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: no-referrer`。
