#!/usr/bin/env bash
# s3clinet 进程管理：PID 文件、优雅停止（SIGTERM / SIGQUIT）、等待退出。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="${S3C_RUN_DIR:-$ROOT/.run}"

mkdir -p "$RUN_DIR"

pid_file() { echo "$RUN_DIR/$1.pid"; }

read_pid() {
  local name=$1
  local f
  f="$(pid_file "$name")"
  [[ -f "$f" ]] || return 1
  cat "$f"
}

is_running() {
  local pid=$1
  kill -0 "$pid" 2>/dev/null
}

# 优雅停止：先发 SIGTERM，超时后 SIGKILL。
graceful_stop() {
  local name=$1
  local timeout=${2:-30}
  local pid f
  f="$(pid_file "$name")"
  [[ -f "$f" ]] || return 0
  pid="$(cat "$f")"
  if ! is_running "$pid"; then
    rm -f "$f"
    return 0
  fi
  echo "[$name] 发送 SIGTERM (pid=$pid)，最多等待 ${timeout}s…"
  kill -TERM "$pid" 2>/dev/null || true
  local i=0
  while is_running "$pid" && (( i < timeout )); do
    sleep 1
    (( i++ )) || true
  done
  if is_running "$pid"; then
    echo "[$name] 超时，发送 SIGKILL"
    kill -KILL "$pid" 2>/dev/null || true
    sleep 1
  fi
  rm -f "$f"
  echo "[$name] 已停止"
}

write_pid() {
  local name=$1
  local pid=$2
  echo "$pid" > "$(pid_file "$name")"
}

wait_http() {
  local url=$1
  local timeout=${2:-60}
  local i=0
  while (( i < timeout )); do
    if curl -sf --max-time 2 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    (( i++ )) || true
  done
  echo "等待 $url 就绪超时 (${timeout}s)" >&2
  return 1
}

export ROOT RUN_DIR

shutdown_timeout() {
  echo "${S3C_SHUTDOWN_TIMEOUT:-30}"
}
