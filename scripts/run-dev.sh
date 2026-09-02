#!/usr/bin/env bash
# 本地开发：启动 server / web /（可选）nginx，PID 写入 .run/
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/process.sh
source "$ROOT/scripts/lib/process.sh"

USE_NGINX=0
for arg in "$@"; do
  case "$arg" in
    --nginx) USE_NGINX=1 ;;
    -h|--help)
      echo "用法: $0 [--nginx]"
      echo "  --nginx  Go 后端监听 8081，nginx(worker_processes=1) 对外 8080"
      exit 0
      ;;
  esac
done

start_server() {
  graceful_stop server "$(shutdown_timeout)"
  cd "$ROOT/server"
  go mod tidy
  go build -o s3clinet-server .
  if (( USE_NGINX )); then
    export S3C_ADDR=127.0.0.1:8081
  else
    export S3C_ADDR="${S3C_ADDR:-127.0.0.1:8080}"
  fi
  # shellcheck disable=SC1091
  [[ -f .env ]] && set -a && source .env && set +a
  ./s3clinet-server >>"$RUN_DIR/server.log" 2>&1 &
  write_pid server $!
  wait_http "http://127.0.0.1:${S3C_ADDR##*:}/api/health" 30
  echo "[server] 已启动 pid=$(read_pid server) addr=$S3C_ADDR"
}

start_web() {
  graceful_stop web 15
  cd "$ROOT/web"
  pnpm install --silent
  pnpm dev >>"$RUN_DIR/web.log" 2>&1 &
  write_pid web $!
  wait_http "http://127.0.0.1:1949/" 60
  echo "[web] 已启动 pid=$(read_pid web) http://127.0.0.1:1949"
}

start_server
start_web
if (( USE_NGINX )); then
  bash "$ROOT/scripts/nginx-local.sh"
fi

echo ""
echo "开发环境已就绪。停止: $ROOT/scripts/graceful-restart.sh stop"
echo "优雅重启: $ROOT/scripts/graceful-restart.sh all"
