# syntax=docker/dockerfile:1

# 可通过 --build-arg 覆盖镜像仓库/代理（国内环境常用）。
ARG GOPROXY=https://proxy.golang.org,direct
ARG NPM_REGISTRY=https://registry.npmjs.org
# 注入到二进制的版本号（main.version）。
ARG VERSION=v1.0.0-20260901182023

###### Web 前端构建 ######
FROM node:22-slim AS web
ARG NPM_REGISTRY
WORKDIR /web
# 先还原依赖再拷贝源码，充分利用层缓存；锁文件一并拷贝保证可复现。
COPY web/package.json web/pnpm-workspace.yaml web/pnpm-lock.yaml ./
RUN npm config set registry "$NPM_REGISTRY" \
 && npm install -g --no-fund pnpm@11
RUN pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm build

###### Go 后端构建 ######
FROM golang:1.26-alpine AS build
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
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 1000 --create-home app \
 && mkdir -p /data /web/dist \
 && chown -R app:app /data
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
  CMD ["/s3clinet-server", "-healthcheck"]
ENTRYPOINT ["/s3clinet-server"]
