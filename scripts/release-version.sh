#!/usr/bin/env bash
# 一键同步版本号到仓库内所有约定文件。
# 用法：
#   ./scripts/release-version.sh                    # 自动生成 v1.0.0-YYYYMMDDHHmmss
#   ./scripts/release-version.sh v1.0.0-20260902120000  # 指定展示版本（带 v）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DISPLAY="${1:-}"
if [[ -z "$DISPLAY" ]]; then
  DISPLAY="v1.0.0-$(date +%Y%m%d%H%M%S)"
fi
if [[ ! "$DISPLAY" =~ ^v1\.0\.0-[0-9]{14}$ ]]; then
  echo "error: version must match v1.0.0-YYYYMMDDHHmmss, got: $DISPLAY" >&2
  exit 1
fi
MACHINE="${DISPLAY#v}"

echo "Syncing version: display=$DISPLAY machine=$MACHINE"

# Makefile VERSION
sed -i "s/^VERSION ?= .*/VERSION ?= $DISPLAY/" Makefile

# Go main + Dockerfile ARG
sed -i "s/var version = \"v[^\"]*\"/var version = \"$DISPLAY\"/" server/main.go
sed -i "s/^ARG VERSION=.*/ARG VERSION=$DISPLAY/" server/Dockerfile

# docker-compose image tag
sed -i "s|image: s3clinet/server:v[^\"]*|image: s3clinet/server:$DISPLAY|" docker-compose.yml

# npm / cargo machine semver
for f in web/package.json desktop/package.json; do
  sed -i "s/\"version\": \"[^\"]*\"/\"version\": \"$MACHINE\"/" "$f"
done
sed -i "s/^version = \"[^\"]*\"/version = \"$MACHINE\"/" desktop/src-tauri/Cargo.toml
sed -i "s/\"version\": \"[^\"]*\"/\"version\": \"$MACHINE\"/" desktop/src-tauri/tauri.conf.json

# README docker 镜像 tag
sed -i "s|s3clinet/server:v[0-9.-]*|s3clinet/server:$DISPLAY|g" README.md
sed -i 's/当前版本 `v1\.0\.0-[0-9]\{14\}`/当前版本 `'"$DISPLAY"'`/' README.md

# docs/API.md health example
sed -i "s/\"version\":\"v[^\"]*\"/\"version\":\"$DISPLAY\"/" docs/API.md

# agents.md 示例版本
sed -i 's/例如 `v1\.0\.0-[0-9]\{14\}`/例如 `'"$DISPLAY"'`/' agents.md

echo "Done. Files updated under $ROOT"
echo "Remember to add a CHANGELOG.md entry for [$DISPLAY]"
