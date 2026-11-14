#!/bin/sh
# gdpm 日采集入口：抓取 -> 入库 -> 一行摘要。
#
# 退出码就是 gdpm_daily.py 的退出码（0 成功 / 1 抓取或入库失败）：
# k8s 侧靠它判失败，失败时库里同时有一行 gd_pm_fetch_run.status='failed'。
# 出口代理只读环境变量 HTTPS_PROXY / HTTP_PROXY（urllib 原生支持），这里不写死地址。
set -e

DB="${GDPM_DB:-/app/out/db.sqlite3}"
WORK_DIR="${GDPM_WORK_DIR:-/tmp/gdpm}"
mkdir -p "$(dirname "$DB")" "$WORK_DIR"

rc=0
python3 /app/gdpm_daily.py --db "$DB" --work-dir "$WORK_DIR" || rc=$?

# 摘要必须打，成功失败都打一遍 —— 这是 CronJob 日志里唯一一行人看的东西。
python3 - "$DB" <<'PY' || true
import sqlite3, sys
db = sys.argv[1]
try:
    conn = sqlite3.connect(db)
    conn.row_factory = sqlite3.Row
    # 按 id 取最新：updated_at 是字符串，+08:00 与 +00:00 混在一起按字典序排会取错行。
    r = conn.execute(
        "SELECT target_date,dataset,status,rows_written,warnings,updated_at"
        " FROM gd_pm_fetch_run ORDER BY id DESC LIMIT 1").fetchone()
    if r:
        warn = (r["warnings"] or "").splitlines()
        tail = (" | " + warn[0][:200]) if warn else ""
        print(f"[gdpm] 最近采集 date={r['target_date']} dataset={r['dataset']}"
              f" status={r['status']} rows={r['rows_written']}"
              f" at={r['updated_at']}{tail}")
    else:
        print("[gdpm] 采集记录为空：本次连留痕都没写进去")
    # 首次跑/抓取失败时指标表可能还不存在，这不是摘要的错，不报。
    try:
        n = conn.execute("SELECT COUNT(*), MAX(run_date) FROM gd_spot_metric"
                         " WHERE origin='pm-api'").fetchone()
        print(f"[gdpm] pm-api 指标共 {n[0]} 条，最新数据日 {n[1]}")
    except Exception:
        print("[gdpm] 指标表尚未建立（本次未写入）")
except Exception as e:
    print(f"[gdpm] 摘要读取失败 {type(e).__name__}: {e}")
PY

exit "$rc"
