#!/usr/bin/env python3
"""gdpm 日采集编排：抓取 -> 入库 -> 失败留痕。

容器的入口。把原来手动的两行命令串起来，并把「抓取失败」变成库里看得见的状态：

    成功：gdpm_dataset.dump() 出长表 -> gdpm_sqlite.apply() 写指标 + status='ok'
    失败：什么都不写指标，只往 gd_pm_fetch_run 补一行 status='failed'，然后非零退出

失败行写进 target_date=当天、dataset='fetch'；不写指标表是有意的 —— 抓失败时手上没有
任何可信数值，写半份数据比不写更糟（下游会当成正常更新推进 dataAsOf）。

退出码：0 成功，1 抓取/入库失败，2 配置或环境错误。
"""

import argparse
import datetime as dt
import os
import sys
import traceback

import gd_pm_fetch as fetch
import gdpm_dataset
import gdpm_sqlite as imp
import gdpm_db as dbc

DATASET_FAILED = "fetch"
WARN_LIMIT = 2000          # warnings 是 TEXT，但别让整段 traceback 灌进去
# 数据日按广东自然日算，不能跟容器时区走：镜像默认 UTC，北京时间 08:00 前跑会记成前一天，
# 和 gd_spot_metric 的 run_date（业务日均按 Asia/Shanghai）错开一天。
TZ_CN = dt.timezone(dt.timedelta(hours=8))


def _today():
    return dt.datetime.now(TZ_CN).strftime("%Y-%m-%d")


def _now():
    # 与 gdpm_sqlite.apply() 同款格式（带偏移，Go 侧按本地时区解析），但固定 +08:00：
    # 镜像里没有 tzdata，astimezone() 会落到 UTC，与业务时间戳混在一起。
    return dt.datetime.now(TZ_CN).isoformat(sep=" ")


def record_failure(db, target_date, message, log=print):
    """抓取失败留痕：只写采集元数据表，一行 status='failed'。

    gd_spot_metric 一行都不碰 —— 失败时没有可信数值。
    留痕本身失败（库路径不可写/只读挂载）不能把进程炸掉：磁盘满、权限错这种时候
    更需要靠非零退出码和 stderr 让人看见，所以这里只报错不抛。
    """
    try:
        conn, pg = dbc.connect(os.environ.get("GDPM_DB_LINK"), db)
    except Exception as e:
        log(f"留痕失败：库打不开 {db} {type(e).__name__}: {e}")
        return
    try:
        conn.execute(dbc.ddl_for(imp.DDL_FETCH_RUN, pg))
        now = _now()
        conn.execute(
            "INSERT INTO gd_pm_fetch_run (target_date,dataset,status,rows_written,source_url,"
            "payload,warnings,extractor_version,created_at,updated_at) "
            "VALUES (?,?,?,?,?,?,?,?,?,?) "
            "ON CONFLICT (target_date,dataset,extractor_version) DO UPDATE SET "
            "status=excluded.status, rows_written=excluded.rows_written,"
            "warnings=excluded.warnings, updated_at=excluded.updated_at",
            (target_date, DATASET_FAILED, "failed", 0,
             fetch.BASE, None,
             message[:WARN_LIMIT], imp.EXTRACTOR, now, now))
        conn.commit()
    except Exception as e:
        log(f"留痕失败：写 gd_pm_fetch_run 出错 {type(e).__name__}: {e}")
        return
    finally:
        conn.close()
    log(f"采集失败已留痕：gd_pm_fetch_run status=failed target_date={target_date}")


def run(db, work_dir, fetch=None, today=None, log=print):
    """跑一次日采集，返回退出码。fetch 可注入（测试用桩，默认真抓）。"""
    fetch = fetch or gdpm_dataset.dump
    today = today or _today()
    long_csv = os.path.join(work_dir, "gdpm_long.csv")

    try:
        os.makedirs(work_dir, exist_ok=True)
        fetch(work_dir, log=log)
        if not os.path.exists(long_csv):
            raise RuntimeError(f"抓取结束但没产出长表: {long_csv}")

        records = imp.load_long(long_csv)
        if not records:
            raise RuntimeError("抓取产出 0 行日频指标（源站可能整站不可用）")
    except Exception as e:
        detail = f"{type(e).__name__}: {e}\n{traceback.format_exc()}".strip()
        record_failure(db, today, detail, log=log)
        log(f"抓取失败，未写任何指标：{type(e).__name__}: {e}")
        return 1

    # 入库沿用原路径：--apply 的语义在这里固定成「总是写」。
    conn, pg = dbc.connect(os.environ.get("GDPM_DB_LINK"), db)
    try:
        # plan() 会先读 gd_spot_metric，新库上表还不存在；建表和 apply 里同一个 DDL。
        imp.ensure_schema(conn, pg)
        periodic, loaded, skipped = imp.load_periodic(work_dir)
        all_records = records + periodic
        p = imp.plan(conn, all_records)
        written = imp.apply(conn, all_records, p, log=log, pg=pg)
    except Exception as e:
        conn.rollback()
        conn.close()
        detail = f"apply 失败 {type(e).__name__}: {e}\n{traceback.format_exc()}".strip()
        record_failure(db, today, detail, log=log)
        log(f"入库失败：{type(e).__name__}: {e}")
        return 1
    finally:
        if conn:
            conn.close()

    days = sorted({r[0] for r in records})
    log(f"采集完成：日频 {len(records)} 行（{days[0]} → {days[-1]}），"
        f"月频/年频 {len(periodic)} 行，入库 {written['metrics']} 条指标、"
        f"{written['fetch_runs']} 条采集元数据")
    return 0


def main():
    ap = argparse.ArgumentParser(description="gdpm 日采集（抓取 -> 入库 -> 失败留痕）")
    ap.add_argument("--db", required=True, help="ems 的 sqlite 路径")
    ap.add_argument("--work-dir", default="/tmp/gdpm", help="中间文件目录（默认 /tmp/gdpm）")
    ap.add_argument("--today", help="失败留痕用的数据日（默认本机当天，测试用）")
    args = ap.parse_args()
    return run(args.db, args.work_dir, today=args.today,
               log=lambda m: print(m, file=sys.stderr))


if __name__ == "__main__":
    sys.exit(main())
