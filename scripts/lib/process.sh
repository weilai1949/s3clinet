#!/usr/bin/env bash
# s3clinet 进程管理：PID 文件、优雅停止（SIGTERM / SIGQUIT）、等待退出。
set -euo pipefail

# 本文件在 scripts/lib/，仓库根为上两级。
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
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

# 各受管进程的预期进程名（PID 复用防护：调用方以 name 传入，这里映射到
# 进程命令行中应出现的关键字）。新增受管进程时在此登记。
expected_process_pattern() {
  case "$1" in
    server)  echo "s3clinet-server" ;;
    web)     echo "vite" ;;
    nginx)   echo "nginx" ;;
    desktop) echo "tauri" ;;
    *)       echo "" ;;
  esac
}

# 校验 pid 对应的进程命令行确实含预期进程名：
#   is_expected_process <pid> <name_pattern>
# Linux 下读 /proc/$pid/cmdline（NUL 分隔，转空格后子串匹配）；
# macOS 无 /proc，回退 ps -p <pid> -o command=。不匹配或进程不存在返回 1。
is_expected_process() {
  local pid=$1
  local name_pattern=${2:-}
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  [[ -n "$name_pattern" ]] || return 1
  local cmd=""
  if [[ -r "/proc/$pid/cmdline" ]]; then
    cmd="$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)"
  else
    # macOS / 无 /proc 环境
    cmd="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  fi
  [[ -n "$cmd" && "$cmd" == *"$name_pattern"* ]]
}

# 读取并校验 PID 文件：进程在跑且确为预期进程才输出 pid；
# 否则视为陈旧 PID 文件，删除并警告，返回 1（不 kill）。
#   validated_pid <name>
validated_pid() {
  local name=$1
  local pid f pattern
  f="$(pid_file "$name")"
  [[ -f "$f" ]] || return 1
  pid="$(cat "$f" 2>/dev/null || true)"
  pattern="$(expected_process_pattern "$name")"
  if is_running "$pid" && is_expected_process "$pid" "$pattern"; then
    echo "$pid"
    return 0
  fi
  if is_running "$pid"; then
    # kill -0 通过但命令行不含预期进程名：PID 被复用，绝不能 kill
    echo "[$name] 警告: pid=$pid 存活但不是预期的 '$pattern' 进程（PID 已被复用？），视为陈旧 PID 文件，删除且不 kill" >&2
  else
    echo "[$name] 警告: pid=$pid 已不存在，清理陈旧 PID 文件" >&2
  fi
  rm -f "$f"
  return 1
}

# 优雅停止：先发 SIGTERM，超时后 SIGKILL。
# 发送任何信号前先校验 PID 确实属于预期进程，防止 PID 复用误杀。
graceful_stop() {
  local name=$1
  local timeout=${2:-30}
  local pid f
  f="$(pid_file "$name")"
  [[ -f "$f" ]] || return 0
  if ! pid="$(validated_pid "$name")"; then
    # validated_pid 已清理陈旧 PID 文件并给出警告
    return 0
  fi
  echo "[$name] 发送 SIGTERM (pid=$pid)，最多等待 ${timeout}s…"
  kill -TERM "$pid" 2>/dev/null || true
  local i=0
  while is_running "$pid" && (( i < timeout )); do
    sleep 1
    (( i++ )) || true
  done
  if is_running "$pid" && is_expected_process "$pid" "$(expected_process_pattern "$name")"; then
    echo "[$name] 超时，发送 SIGKILL"
    kill -KILL "$pid" 2>/dev/null || true
    sleep 1
  elif is_running "$pid"; then
    echo "[$name] 警告: 等待期间 pid=$pid 进程身份变化，跳过 SIGKILL（可能已被 PID 复用）" >&2
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
