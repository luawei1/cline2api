#!/usr/bin/env sh
# 一键更新：拉取代码 → 跑测试 → 重新构建 → 重启服务（带健康检查）。
#
# 用法:
#   ./update.sh               拉取 origin/main 并更新
#   ./update.sh --upstream     额外快进合并 upstream/main（需先配置 upstream 远端）
#   ./update.sh --no-pull      跳过拉取，只测试 + 构建 + 重启
#   ./update.sh --no-test      跳过测试（不推荐）
#
# 说明: 拉取/测试/构建阶段旧进程继续运行；只有全部成功后才重启，
#       任一步失败都会保留正在运行的旧版本。
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"

BIN=./cline-proxy
LOG=cline-proxy.log
HEALTH_URL=${HEALTH_URL:-http://127.0.0.1:3457/admin/}
PULL=1
TESTS=1
USE_UPSTREAM=0

for arg in "$@"; do
  case "$arg" in
    --no-pull)   PULL=0 ;;
    --no-test)   TESTS=0 ;;
    --upstream)  USE_UPSTREAM=1 ;;
    -h|--help)   sed -n '2,12p' "$0"; exit 0 ;;
    *) echo "unknown option: $arg (try --help)" >&2; exit 2 ;;
  esac
done

say() { printf '\n==> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || die "go not found in PATH"

if [ "$PULL" = 1 ]; then
  say "Pulling updates (origin)"
  git pull --ff-only
  if [ "$USE_UPSTREAM" = 1 ]; then
    if git remote get-url upstream >/dev/null 2>&1; then
      say "Pulling updates (upstream)"
      git fetch upstream
      git merge --ff-only upstream/main
    else
      echo "note: 'upstream' remote not configured, skipping" >&2
    fi
  fi
fi

if [ "$TESTS" = 1 ]; then
  say "Running tests"
  # 隔离测试的数据目录：单测会通过 setProxyConfig/upsertProvider 落盘，若数据目录
  # 仍是仓库根目录，会覆盖用户真实的 .cline-config.json（模型组合/配置）等文件。
  # 注意：各包的 TestMain 也会重定向；这里是覆盖 `./...` 全部包的额外防线。
  TEST_DATA_DIR=$(mktemp -d)
  trap 'rm -rf "$TEST_DATA_DIR"' EXIT
  CLINE2API_DATA_DIR="$TEST_DATA_DIR" go test ./...
fi

say "Building"
VERSION=${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}
go build -trimpath -ldflags "-s -w -X main.appVersion=$VERSION" -o "$BIN" .
printf 'Built: %s (version %s)\n' "$BIN" "$VERSION"

say "Restarting service"
if pkill -x cline-proxy 2>/dev/null; then
  i=0
  while pgrep -x cline-proxy >/dev/null 2>&1 && [ "$i" -lt 20 ]; do
    sleep 0.5
    i=$((i + 1))
  done
  pgrep -x cline-proxy >/dev/null 2>&1 && die "old instance did not stop"
  echo "old instance stopped"
else
  echo "no instance was running"
fi

# setsid: 独立会话启动，避免脚本所在 shell 退出时被一并回收
setsid nohup "$BIN" > "$LOG" 2>&1 < /dev/null &
sleep 1
NEW_PID=$(pgrep -x cline-proxy | head -1 || true)
[ -n "$NEW_PID" ] || { tail -20 "$LOG" >&2; die "failed to start $BIN"; }
echo "started pid=$NEW_PID (log: $LOG)"

say "Health check ($HEALTH_URL)"
i=0
while [ "$i" -lt 30 ]; do
  if ! pgrep -x cline-proxy >/dev/null 2>&1; then
    echo "--- last log lines:" >&2
    tail -20 "$LOG" >&2
    die "process exited during startup"
  fi
  code=$(curl -s -o /dev/null -w '%{http_code}' "$HEALTH_URL" || echo 000)
  [ "$code" = "200" ] && break
  sleep 1
  i=$((i + 1))
done

if [ "$code" = "200" ]; then
  printf 'OK: %s -> HTTP %s\n' "$HEALTH_URL" "$code"
else
  echo "warning: health check did not return 200 (got $code)" >&2
  echo "server may be bound to another host — check $LOG" >&2
fi

printf '\nDone. Admin: %s\n' "$HEALTH_URL"
