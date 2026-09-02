#!/usr/bin/env bash
# s3clinet 优雅重启：server / web / desktop / nginx / docker / all
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/process.sh
source "$ROOT/scripts/lib/process.sh"

usage() {
  cat <<'EOF'
用法: graceful-restart.sh <命令>

命令:
  server    优雅重启 Go 后端（SIGTERM → 等待 Shutdown → 再启动）
  web       优雅重启 Vite 开发服务器
  desktop   优雅重启 Tauri dev（若曾通过 run-dev 或本脚本启动）
  nginx     nginx 优雅 reload（worker_processes=1，不中断已有连接）
            或 Docker 内 nginx -s reload
  docker    docker compose 优雅滚动 server + nginx（stop_grace_period）
  all       依次 reload nginx → 重启 server → 重启 web
  stop      停止 .run/ 下所有本地进程
  status    查看 PID 与 HTTP 健康

环境变量:
  S3C_SHUTDOWN_TIMEOUT  Go 关停等待秒数（默认 30）
  S3C_RUN_DIR           PID/日志目录（默认 .run/）
EOF
}

restart_server() {
  local t
  t="$(shutdown_timeout)"
  graceful_stop server "$t"
  cd "$ROOT/server"
  if [[ ! -x ./s3clinet-server ]]; then
    go mod tidy
    go build -o s3clinet-server .
  fi
  # shellcheck disable=SC1091
  [[ -f .env ]] && set -a && source .env && set +a
  export S3C_ADDR="${S3C_ADDR:-127.0.0.1:8080}"
  ./s3clinet-server >>"$RUN_DIR/server.log" 2>&1 &
  write_pid server $!
  wait_http "http://127.0.0.1:${S3C_ADDR##*:}/api/health" 30
  echo "[server] 已优雅重启 pid=$(read_pid server)"
}

restart_web() {
  graceful_stop web 15
  cd "$ROOT/web"
  pnpm dev >>"$RUN_DIR/web.log" 2>&1 &
  write_pid web $!
  wait_http "http://127.0.0.1:1949/" 60
  echo "[web] 已优雅重启 pid=$(read_pid web)"
}

restart_desktop() {
  graceful_stop desktop 20
  cd "$ROOT/desktop"
  pnpm tauri dev >>"$RUN_DIR/desktop.log" 2>&1 &
  write_pid desktop $!
  echo "[desktop] 已启动 pid=$(read_pid desktop)（日志: $RUN_DIR/desktop.log）"
}

reload_nginx() {
  # Docker Compose nginx
  if docker compose -f "$ROOT/docker-compose.yml" ps nginx 2>/dev/null | grep -q 'Up'; then
    echo "[nginx] docker compose exec nginx -s reload"
    docker compose -f "$ROOT/docker-compose.yml" exec -T nginx nginx -s reload
    return 0
  fi
  # 本机 nginx（run-dev --nginx 或手动启动）
  local pid
  if pid="$(read_pid nginx 2>/dev/null)" && is_running "$pid"; then
    echo "[nginx] 本机 reload (pid=$pid)"
    nginx -s reload -g "pid $RUN_DIR/nginx.pid;" 2>/dev/null || kill -HUP "$pid"
    return 0
  fi
  if [[ -f "$RUN_DIR/nginx.pid" ]] && is_running "$(cat "$RUN_DIR/nginx.pid")"; then
    nginx -s reload -g "pid $RUN_DIR/nginx.pid;"
    echo "[nginx] 已 reload"
    return 0
  fi
  echo "[nginx] 未检测到运行中的 nginx，跳过" >&2
  return 0
}

restart_docker() {
  local t
  t="$(shutdown_timeout)"
  cd "$ROOT"
  echo "[docker] 优雅停止 server/nginx（${t}s grace）…"
  docker compose stop -t "$t" nginx server 2>/dev/null || docker compose stop -t "$t" server
  docker compose up -d --build server
  if docker compose config --services 2>/dev/null | grep -qx nginx; then
    docker compose up -d nginx
    reload_nginx
  fi
  wait_http "http://127.0.0.1:8080/api/health" 90
  echo "[docker] 已滚动重启"
}

stop_all() {
  reload_nginx 2>/dev/null || true
  graceful_stop nginx 15
  graceful_stop desktop 20
  graceful_stop web 15
  graceful_stop server "$(shutdown_timeout)"
  echo "全部本地进程已停止"
}

cmd_status() {
  for name in server web desktop nginx; do
    if pid="$(read_pid "$name" 2>/dev/null)" && is_running "$pid"; then
      echo "$name: running pid=$pid"
    else
      echo "$name: stopped"
    fi
  done
  curl -sf --max-time 2 "http://127.0.0.1:8080/api/health" && echo "health(8080): ok" || echo "health(8080): fail"
  curl -sf --max-time 2 "http://127.0.0.1:1949/" >/dev/null && echo "web(1949): ok" || echo "web(1949): fail"
}

cmd="${1:-}"
case "$cmd" in
  server)  restart_server ;;
  web)     restart_web ;;
  desktop) restart_desktop ;;
  nginx)   reload_nginx ;;
  docker)  restart_docker ;;
  all)
    reload_nginx || true
    restart_server
    restart_web
    ;;
  stop)    stop_all ;;
  status)  cmd_status ;;
  -h|--help|"") usage ;;
  *) echo "未知命令: $cmd" >&2; usage; exit 1 ;;
esac
