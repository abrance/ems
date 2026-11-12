#!/bin/sh
# lems SQLite 在线备份：一致性快照 → 校验 → 异地 → 按保留策略清理本地。
#
# 为什么不能 cp：v1.0.65 起库是 WAL 模式（out/ 下有 db.sqlite3-wal/-shm），
# 直接拷主文件会拿到不含最近写入的残缺快照，且恢复时不报错。
# 这里用 SQLite 原生 Backup API（sqlite3 ".backup"），在线一致、不停容器。
#
# 跑在宿主机（Debian），不需要 sudo：库文件通过 docker 卷只读挂进容器读。
# 失败一律非零退出，cron 日志里能直接看出来。
set -eu

# ---- 可调参数：只改这一段 ----
BACKUP_DIR="${BACKUP_DIR:-/opt/cops/backup/lems}"         # 本地备份目录
OFFSITE="${OFFSITE-nas:/pool1_8t/public/backup/lems}"    # 异地目标 ssh:path；显式置空（OFFSITE=）= 不做异地
KEEP_DAILY="${KEEP_DAILY:-7}"                             # 本地保留日备份数
KEEP_WEEKLY="${KEEP_WEEKLY:-4}"                           # 本地保留周备份数（UTC 周日那份）
# ------------------------------

VOLUME="${VOLUME:-lems_lems-data}"   # 生产命名卷；演练时用 VOLUME=lems-drill_lems-data
DB="${DB:-db.sqlite3}"
SQLITE_IMAGE="${SQLITE_IMAGE:-ems-backup-tools:1}"  # 只需带 sqlite3 的小镜像
TABLES="${TABLES:-alarm_history log device price setting}"

TS=$(date -u +%Y%m%dT%H%M%SZ)
NAME="db-${TS}.sqlite3.gz"
PART=".db-${TS}.sqlite3.part"
LOG_PREFIX="[backup $(date '+%F %T %Z')]"

die() { echo "$LOG_PREFIX FAILED: $*" >&2; exit 1; }

mkdir -p "$BACKUP_DIR"

# 同一时刻只允许一个备份在跑，避免两份同时压库/互相覆盖
exec 9>"${BACKUP_DIR}/.backup.lock"
flock -n 9 || die "另一个备份正在进行（锁 ${BACKUP_DIR}/.backup.lock）"

# 容器内 sqlite3；库目录只读挂载，备份产物写 /dst
sq() { docker run --rm -v "${VOLUME}:/src:ro" -v "${BACKUP_DIR}:/dst" \
         --entrypoint sqlite3 "$SQLITE_IMAGE" "$@"; }

# 1) 一致性快照
echo "$LOG_PREFIX snapshot ${VOLUME}/${DB} -> ${BACKUP_DIR}/${NAME}"
sq "/src/${DB}" ".backup '/dst/${PART}'" || die "sqlite3 .backup 失败"

# 2) 校验：integrity_check + 关键表行数（行数落进 .last-backup 供事后比对）
ROWS=$(sq "/dst/${PART}" "PRAGMA integrity_check; $(for t in $TABLES; do echo "select '$t=' || count(*) from $t;"; done)" 2>&1) \
  || { rm -f "${BACKUP_DIR}/${PART}"; die "校验查询失败"; }
[ "$(printf '%s\n' "$ROWS" | head -n1)" = "ok" ] \
  || { rm -f "${BACKUP_DIR}/${PART}"; die "integrity_check 未通过: $ROWS"; }
COUNTS=$(printf '%s\n' "$ROWS" | tail -n +2 | tr '\n' ' ')

# 3) 压缩后落名（快照阶段用 .part，避免半成品被当成可用备份）
gzip -c "${BACKUP_DIR}/${PART}" > "${BACKUP_DIR}/${NAME}" || { rm -f "${BACKUP_DIR}/${PART}" "${BACKUP_DIR}/${NAME}"; die "gzip 失败"; }
rm -f "${BACKUP_DIR}/${PART}"
SIZE=$(stat -c%s "${BACKUP_DIR}/${NAME}")

# 4) 异地：先传成功，再动本地老备份
if [ -n "$OFFSITE" ]; then
  OFF_HOST=${OFFSITE%%:*}
  OFF_PATH=${OFFSITE#*:}
  scp -q -o BatchMode=yes -o ConnectTimeout=10 "${BACKUP_DIR}/${NAME}" "${OFFSITE}/" \
    || die "异地传输失败（本地 ${NAME} 已保留）: ${OFFSITE}"
  # 按字节数核对，避免半截文件被当成备份
  REMOTE_SIZE=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "$OFF_HOST" "stat -c%s '${OFF_PATH}/${NAME}'" 2>/dev/null || echo 0)
  [ "$REMOTE_SIZE" = "$SIZE" ] || die "异地文件大小不符（本地 ${SIZE} / 异地 ${REMOTE_SIZE}），本地已保留"
  echo "$LOG_PREFIX offsite ok ${OFFSITE}/${NAME} bytes=${SIZE}"
fi

# 5) 本地保留：最新 KEEP_DAILY 份全留，另外每天周日（UTC）那份额外留 KEEP_WEEKLY 份
#    只有异地成功（或未配置异地）才走到这里：异地挂了不会删掉本地唯一的副本
keep_list=$(ls -1t "${BACKUP_DIR}"/db-*.sqlite3.gz 2>/dev/null | head -n "$KEEP_DAILY")
weekly=$(ls -1t "${BACKUP_DIR}"/db-*.sqlite3.gz 2>/dev/null \
  | while read -r f; do
      d=$(basename "$f" | sed 's/^db-\([0-9]\{8\}\)T.*/\1/')
      [ "$(date -u -d "$d" +%u)" = "7" ] && echo "$f"
    done | head -n "$KEEP_WEEKLY")
for f in $(ls -1t "${BACKUP_DIR}"/db-*.sqlite3.gz 2>/dev/null); do
  printf '%s\n%s\n' "$keep_list" "$weekly" | grep -qxF "$f" && continue
  echo "$LOG_PREFIX prune $(basename "$f")"
  rm -f "$f"
done

echo "$LOG_PREFIX OK ${NAME} size=${SIZE} ${COUNTS}"
# 状态落一份在宿主机目录，容器/日志丢了它也在
printf '%s OK %s size=%s %s\n' "$(date '+%F %T %Z')" "${NAME}" "${SIZE}" "${COUNTS}" > "${BACKUP_DIR}/.last-backup"
