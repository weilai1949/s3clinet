#!/usr/bin/env bash
# 本机 nginx（--nginx 模式）：worker_processes=1，需在 repo 根目录执行
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/process.sh
source "$ROOT/scripts/lib/process.sh"

command -v nginx >/dev/null || { echo "未找到 nginx" >&2; exit 1; }

mkdir -p "$RUN_DIR"
LOCAL_CONF="$ROOT/deploy/nginx/nginx.local.conf"
# pid 路径在配置里为相对路径 .run/nginx.pid
sed "s|pid .run/nginx.pid;|pid $RUN_DIR/nginx.pid;|" "$LOCAL_CONF" > "$RUN_DIR/nginx.effective.conf"

nginx -t -p "$ROOT" -c "$RUN_DIR/nginx.effective.conf"
if pid="$(read_pid nginx 2>/dev/null)" && is_running "$pid"; then
  nginx -s reload -p "$ROOT" -c "$RUN_DIR/nginx.effective.conf"
  echo "[nginx] reload ok (worker_processes=1)"
else
  nginx -p "$ROOT" -c "$RUN_DIR/nginx.effective.conf"
  write_pid nginx "$(cat "$RUN_DIR/nginx.pid")"
  echo "[nginx] started pid=$(read_pid nginx)"
fi
