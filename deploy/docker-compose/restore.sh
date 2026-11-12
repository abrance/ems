#!/bin/sh
# lems SQLite 从备份恢复。
#
# 顺序：停容器 → 现存库改名留底（不删）→ 清 -wal/-shm → 放回备份 → 核行数 → 起容器 → 验 health
# -wal/-shm 必须删：残留 WAL 会被新库当成未提交事务，回放后数据错乱。
#
# 用法：./restore.sh /opt/cops/backup/lems/db-20261112T120000Z.sqlite3.gz
# 演练：PROJECT=lems-drill VOLUME=lems-drill_lems-data CONTAINER=lems-drill \
#         HEALTH_URL=http://127.0.0.1:18080/api/health ./restore.sh <file>
set -eu

BK="${1:?用法: $0 <backup.sqlite3.gz>}"
[ -f "$BK" ] || { echo "备份文件不存在: $BK" >&2; exit 1; }

PROJECT="${PROJECT:-lems}"                       # compose 项目名
CONTAINER="${CONTAINER:-lems}"                   # 容器名
VOLUME="${VOLUME:-lems_lems-data}"               # 数据卷
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:80/api/health}"
SQLITE_IMAGE="${SQLITE_IMAGE:-ems-backup-tools:1}"
DB="${DB:-db.sqlite3}"
TABLES="${TABLES:-alarm_history log device price setting}"
OWNER="${OWNER:-1001:1001}"                      # 卷内属主，与生产一致
WAIT="${WAIT:-120}"                              # health 等待上限（秒）

TS=$(date -u +%Y%m%dT%H%M%SZ)
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
COUNTS_SQL=$(for t in $TABLES; do echo "select '$t=' || count(*) from $t;"; done)

# 只读挂载下必须用 immutable=1，否则 sqlite 想建 journal 会报 unable to open database file
echo "[restore] 解压并校验 $BK"
gzip -dc "$BK" > "${STAGE}/${DB}"
docker run --rm -v "${STAGE}:/src:ro" --entrypoint sqlite3 "$SQLITE_IMAGE" \
  "file:/src/${DB}?immutable=1" "PRAGMA integrity_check;" | head -n1 | grep -qx ok \
  || { echo "[restore] 备份完整性校验失败，未做任何改动" >&2; exit 1; }
WANT=$(docker run --rm -v "${STAGE}:/src:ro" --entrypoint sqlite3 "$SQLITE_IMAGE" \
  "file:/src/${DB}?immutable=1" "$COUNTS_SQL" | tr '\n' ' ')
echo "[restore] 备份内容: $WANT"

echo "[restore] 停 ${CONTAINER}"
# 容器还没建过（首次恢复）时不停不报错，后面 docker start 会把它拉起来
docker stop "$CONTAINER" >/dev/null 2>&1 || echo "[restore] ${CONTAINER} 当前未运行，跳过停止"

# 现存库改名留底，绝不删；-wal/-shm 直接丢弃（属于旧库，回放会污染新库）
docker run --rm -v "${VOLUME}:/d" -v "${STAGE}:/src:ro" --entrypoint sh "$SQLITE_IMAGE" -c "
  set -e
  if [ -f /d/${DB} ]; then mv /d/${DB} /d/${DB}.pre-restore-${TS}; echo '  kept /d/${DB}.pre-restore-${TS}'; fi
  rm -f /d/${DB}-wal /d/${DB}-shm
  cp /src/${DB} /d/${DB}
  chown ${OWNER} /d/${DB}
  ls -l /d/${DB}
" 2>&1 | sed 's/^/[restore] /'

# 容器还停着时核行数：此刻库是静止的，读出来的数才可信
GOT=$(docker run --rm -v "${VOLUME}:/src:ro" --entrypoint sqlite3 "$SQLITE_IMAGE" \
  "file:/src/${DB}?immutable=1" "$COUNTS_SQL" | tr '\n' ' ')
echo "[restore] 恢复后行数: $GOT"
[ "$GOT" = "$WANT" ] || { echo "[restore] 行数与备份不一致！期望: $WANT —— 容器保持停止，请人工确认" >&2; exit 1; }

echo "[restore] 起 ${CONTAINER}"
docker start "$CONTAINER" >/dev/null

echo "[restore] 等 health（最多 ${WAIT}s）"
i=0; code=""
while [ "$i" -lt "$WAIT" ]; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$HEALTH_URL" || true)
  [ "$code" = "200" ] && break
  i=$((i + 1)); sleep 1
done
[ "$code" = "200" ] || { echo "[restore] health 未返回 200（最后 ${code:-无响应}），请查 docker logs ${CONTAINER}" >&2; exit 1; }
echo "[restore] OK health=200 $GOT"
