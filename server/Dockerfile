# syntax=docker/dockerfile:1

# 可通过 --build-arg 覆盖镜像仓库/代理（国内环境常用）。
ARG GOPROXY=https://proxy.golang.org,direct
ARG NPM_REGISTRY=https://registry.npmjs.org
# 注入到二进制的版本号（main.version）。
ARG VERSION=v1.0.0-rc1

###### Web 前端构建 ######
# Alpine + Node 24 LTS：体积小、漏洞面窄，构建产物只复制 dist/ 到运行时。
FROM node:24-alpine AS web
ARG NPM_REGISTRY
WORKDIR /web
# 先还原依赖再拷贝源码，充分利用层缓存；锁文件一并拷贝保证可复现。
COPY web/package.json web/pnpm-workspace.yaml web/pnpm-lock.yaml ./
RUN npm config set registry "$NPM_REGISTRY" \
 && npm install -g --no-fund pnpm@9.15.0
RUN pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm build

###### Go 后端构建 ######
# 固定 patch 版本（go.mod 要求 1.26.5）；alpine 镜像无 shell/apt 包，
# 仅产出静态二进制供运行时拷贝。
FROM golang:1.26.5-alpine AS build
ARG GOPROXY
ARG VERSION
RUN apk add --no-cache ca-certificates
WORKDIR /src
ENV GOPROXY=$GOPROXY
# 先还原模块再拷贝源码，充分利用层缓存
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/s3clinet-server .

###### 运行时 ######
# 关键修复：debian:bookworm-slim 体积大、glibc + openssl 漏洞面广（Trivy 反复告警）。
# 改用 alpine:3.20 + 仅 ca-certificates + wget（供 HEALTHCHECK）：
#   - musl libc 比 glibc 漏洞面窄
#   - alpine 的 OpenSSL 受 Alpine 安全团队快速维护
#   - 体积约 5MB（vs debian-slim ~80MB）
#   - 仍保留 minimal busybox 工具用于 HEALTHCHECK 与调试
FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget \
 && addgroup -g 1000 -S app \
 && adduser -u 1000 -S -G app -h /home/app app \
 && mkdir -p /data /web/dist \
 && chown -R app:app /data /web/dist
COPY --from=build /out/s3clinet-server /s3clinet-server
COPY --from=web /web/dist /web/dist
ENV S3C_ADDR=0.0.0.0:8080 \
    S3C_STATIC_DIR=/web/dist \
    S3C_DATA_DIR=/data \
    S3C_REGION=us-east-1
VOLUME ["/data"]
WORKDIR /
USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -q -O- http://127.0.0.1:8080/api/health >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/s3clinet-server"]
