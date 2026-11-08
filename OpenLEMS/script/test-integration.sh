#!/bin/bash
# 集成测试入口：只连接 .env.integration 指定的中间件，不启动/停止它们。
# 规范见 docs/testing-standard.md。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./workspace-common.sh
source "$SCRIPT_DIR/workspace-common.sh"

ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env.integration}"
FILTER="${1:-}"

die() {
  echo "错误：$*" >&2
  exit 2
}

[ -f "$ENV_FILE" ] || die "缺少 $ENV_FILE。先执行：cp .env.integration.example .env.integration，并填写中间件地址"

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

TEST_ENV="${TEST_ENV:-local}"
case "$TEST_ENV" in
  local | dev | ci) ;;
  prod | production) die "TEST_ENV=$TEST_ENV：集成测试不允许指向生产环境" ;;
  *) die "TEST_ENV=$TEST_ENV 无效，只允许 local / dev / ci" ;;
esac

case "${TEST_DB_DRIVER:-}" in
  sqlite | mysql | postgres) ;;
  "") die "TEST_DB_DRIVER 未设置（sqlite / mysql / postgres）" ;;
  *) die "TEST_DB_DRIVER=${TEST_DB_DRIVER} 不支持" ;;
esac

[ -n "${TEST_DB_DSN:-}" ] || die "TEST_DB_DSN 未设置"
if [ "$TEST_DB_DRIVER" != "sqlite" ] && [ -z "${TEST_DB_SCHEMA:-}" ]; then
  die "TEST_DB_SCHEMA 未设置（mysql/postgres 需要独立测试库名做隔离）"
fi

case "${TEST_DB_DSN}${TEST_HTTP_BASE_URL:-}" in
  *prod* | *PROD* | *Prod* | *production*) die "连接串指向生产环境，拒绝执行" ;;
esac

export LOG_LEVEL="${LOG_LEVEL:-warn}"
export TEST_ENV TEST_DB_DRIVER TEST_DB_DSN TEST_DB_SCHEMA
export TEST_REDIS_ADDR="${TEST_REDIS_ADDR:-}" TEST_REDIS_DB="${TEST_REDIS_DB:-}"
export TEST_MQTT_BROKER="${TEST_MQTT_BROKER:-}" TEST_MODBUS_TCP_ADDR="${TEST_MODBUS_TCP_ADDR:-}"
export TEST_HTTP_BASE_URL="${TEST_HTTP_BASE_URL:-}"

TEST_TIMEOUT="${TEST_TIMEOUT:-10m}"
ARTIFACT_DIR="${TEST_ARTIFACT_DIR:-out/test-artifacts}"
case "$ARTIFACT_DIR" in
  /*) ;;
  *) ARTIFACT_DIR="$ROOT_DIR/$ARTIFACT_DIR" ;;
esac
mkdir -p "$ARTIFACT_DIR"
export TEST_ARTIFACT_DIR="$ARTIFACT_DIR"

echo "集成测试：env=$TEST_ENV db=$TEST_DB_DRIVER log=$LOG_LEVEL"

prepare_hexlib

found=0
for mod in $(workspace_modules); do
  plan_dir="$ROOT_DIR/${mod#./}/test_plan"
  [ -d "$plan_dir" ] || continue
  # 只有 .md 没有可执行用例的目录跳过，避免 go test 报 "no Go files"
  ls "$plan_dir"/*_test.go >/dev/null 2>&1 || continue

  run_pattern=""
  if [ -n "$FILTER" ]; then
    run_pattern="${FILTER//-/_}"
  fi

  echo "==> ${mod#./}/test_plan ${run_pattern:+(只跑 $FILTER)}"
  if [ -n "$run_pattern" ]; then
    ( cd "$ROOT_DIR/${mod#./}" && go test -tags=integration -count=1 -timeout "$TEST_TIMEOUT" -run "$run_pattern" ./test_plan/... )
  else
    ( cd "$ROOT_DIR/${mod#./}" && go test -tags=integration -count=1 -timeout "$TEST_TIMEOUT" ./test_plan/... )
  fi
  found=1
done

if [ "$found" = 0 ]; then
  echo "未发现可执行的集成场景。约定见 test_plan/README.md 与 docs/testing-standard.md。"
fi
