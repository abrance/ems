#!/bin/sh
# lems SQLite 从备份恢复。
#
# 顺序：预检（解压 + 完整性 + 行数，任何一步失败都不碰容器）→ 生产护栏 →
#       停容器 → 现存库改名留底（不删）→ 清 -wal/-shm → 放回备份 → 核行数 → 起容器 → 验 health
# -wal/-shm 必须删：残留 WAL 会被新库当成未提交事务，回放后数据错乱。
#
# 用法：./restore.sh <backup.sqlite3.gz> [--yes]
# 目标是生产（CONTAINER=lems 或 VOLUME=lems_lems-data）时必须显式 --yes（或 PROD_CONFIRM=1）。
# 演练：PROJECT=lems-drill VOLUME=lems-drill_lems-data CONTAINER=lems-drill \
#         HEALTH_URL=http://127.0.0.1:18080/api/health ./restore.sh <file>
set -eu

usage() { echo "用法: $0 <backup.sqlite3.gz> [--yes]" >&2; exit 2; }

BK=""
YES="${PROD_CONFIRM:-0}"
for a in "$@"; do
  case "$a" in
    --yes) YES=1 ;;
    -h|--help) usage ;;
    -*) echo "未知参数: $a" >&2; usage ;;
    *) [ -z "$BK" ] || usage; BK="$a" ;;
  esac
done
[ -n "$BK" ] || usage
[ -f "$BK" ] || { echo "备份文件不存在: $BK" >&2; exit 1; }

PROJECT="${PROJECT:-lems}"                       # compose 项目名
CONTAINER="${CONTAINER:-lems}"                   # 容器名
VOLUME="${VOLUME:-lems_lems-data}"               # 数据卷
SQLITE_IMAGE="${SQLITE_IMAGE:-ems-backup-tools:1}"
DB="${DB:-db.sqlite3}"
TABLES="${TABLES:-alarm_history device price setting}"
OWNER="${OWNER:-1001:1001}"                      # 卷内属主，与生产一致
WAIT="${WAIT:-120}"                              # health 等待上限（秒）
LEMS_ENV="${LEMS_ENV:-}" # 生产 env（可选），用来取宿主机映射端口

# 生产护栏：默认值就是生产，没带 --yes 的默认执行必须先说清楚要动什么再退出
if [ "$YES" != "1" ] && { [ "$CONTAINER" = "lems" ] || [ "$VOLUME" = "lems_lems-data" ]; }; then
  echo "[restore] 目标是生产：本次将停止容器 ${CONTAINER}、把 ${VOLUME}/${DB} 改名留底后替换。" >&2
  echo "[restore] 确认要动生产请加 --yes（或 PROD_CONFIRM=1）重跑；演练请显式给 PROJECT/VOLUME/CONTAINER。" >&2
  exit 1
fi

# health 端口：容器内是 80，宿主机映射到 LEMS_PORT（生产 11000）。默认值必须跟着生产走，
# 否则真出事恢复成功也会被判 health 失败退出 1。可用 HEALTH_URL 直接覆盖。
LEMS_PORT="${LEMS_PORT:-}"
if [ -z "$LEMS_PORT" ] && [ -f "$LEMS_ENV" ]; then
  LEMS_PORT=$(sed -n 's/^LEMS_PORT=//p' "$LEMS_ENV" | tail -n1)
fi
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:${LEMS_PORT:-11000}/api/health}"

TS=$(date -u +%Y%m%dT%H%M%SZ)
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
COUNTS_SQL=$(for t in $TABLES; do echo "select '$t=' || count(*) from $t;"; done)

# 所有 docker 输出先落文件再打印：`docker run ... | sed` 在 sh 下没有 pipefail，
# 失败会被 sed 的 0 吃掉，mv/cp/chown 挂掉就静默往下走。
# 只读挂载下必须用 immutable=1，否则 sqlite 想建 journal 会报 unable to open database file
sqlite_snapshot() {
  docker run --rm -v "${STAGE}:/src:ro" --entrypoint sqlite3 "$SQLITE_IMAGE" \
    "file:/src/${DB}?immutable=1" "$1"
}

echo "[restore] 解压并校验 $BK"
gzip -dc "$BK" > "${STAGE}/${DB}" || { echo "[restore] 解压失败，未做任何改动" >&2; exit 1; }
sqlite_snapshot "PRAGMA integrity_check;" > "${STAGE}/check.txt" 2>"${STAGE}/check.err" \
  || { echo "[restore] 校验查询执行失败，未做任何改动: $(tail -n1 "${STAGE}/check.err")" >&2; exit 1; }
[ "$(head -n1 "${STAGE}/check.txt")" = "ok" ] \
  || { echo "[restore] 备份完整性校验失败，未做任何改动: $(head -n1 "${STAGE}/check.txt")" >&2; exit 1; }
sqlite_snapshot "$COUNTS_SQL" > "${STAGE}/want.txt" 2>"${STAGE}/want.err" \
  || { echo "[restore] 行数查询执行失败，未做任何改动: $(tail -n1 "${STAGE}/want.err")" >&2; exit 1; }
WANT=$(tr '\n' ' ' < "${STAGE}/want.txt")
echo "[restore] 备份内容: $WANT"

# 预检全过才碰容器；此后任何失败都要明确告诉人怎么把生产拉回来
STOPPED=0
fail() {
  echo "[restore] $*" >&2
  if [ "$STOPPED" = "1" ]; then
    echo "[restore] 容器 ${CONTAINER} 仍处于停止状态，执行 'docker start ${CONTAINER}' 恢复（现存库已改名留底，未删除）。" >&2
  fi
  exit 1
}

echo "[restore] 停 ${CONTAINER}"
# 容器还没建过（首次恢复）时不停不报错，后面 docker start 会把它拉起来
docker stop "$CONTAINER" >/dev/null 2>&1 || echo "[restore] ${CONTAINER} 当前未运行，跳过停止"
STOPPED=1

# 现存库改名留底，绝不删；-wal/-shm 直接丢弃（属于旧库，回放会污染新库）
docker run --rm -v "${VOLUME}:/d" -v "${STAGE}:/src:ro" --entrypoint sh "$SQLITE_IMAGE" -c "
  set -e
  if [ -f /d/${DB} ]; then mv /d/${DB} /d/${DB}.pre-restore-${TS}; echo '  kept /d/${DB}.pre-restore-${TS}'; fi
  rm -f /d/${DB}-wal /d/${DB}-shm
  cp /src/${DB} /d/${DB}
  chown ${OWNER} /d/${DB}
  ls -l /d/${DB}
" > "${STAGE}/place.log" 2>&1 || fail "放回备份失败（未启动容器）：$(tail -n3 "${STAGE}/place.log" | tr '\n' ' ')"
sed 's/^/[restore] /' "${STAGE}/place.log"

# 容器还停着时核行数：此刻库是静止的，读出来的数才可信
docker run --rm -v "${VOLUME}:/src:ro" --entrypoint sqlite3 "$SQLITE_IMAGE" \
  "file:/src/${DB}?immutable=1" "$COUNTS_SQL" > "${STAGE}/got.txt" 2>"${STAGE}/got.err" \
  || fail "恢复后行数查询失败: $(tail -n1 "${STAGE}/got.err")"
GOT=$(tr '\n' ' ' < "${STAGE}/got.txt")
echo "[restore] 恢复后行数: $GOT"
[ "$GOT" = "$WANT" ] || fail "行数与备份不一致！期望: $WANT / 实际: $GOT"
STOPPED=0

echo "[restore] 起 ${CONTAINER}"
docker start "$CONTAINER" >/dev/null

echo "[restore] 等 health（最多 ${WAIT}s）$HEALTH_URL"
i=0; code=""
while [ "$i" -lt "$WAIT" ]; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$HEALTH_URL" || true)
  [ "$code" = "200" ] && break
  i=$((i + 1)); sleep 1
done
[ "$code" = "200" ] || fail "health 未返回 200（最后 ${code:-无响应}），容器已启动，请查 docker logs ${CONTAINER}"
echo "[restore] OK health=200 $GOT"
