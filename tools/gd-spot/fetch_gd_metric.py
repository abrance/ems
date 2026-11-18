#!/usr/bin/env python3
"""第三方现货站 15 分钟数据 → gd_metric.csv 同格式导出。

背景
----
EMS 首页「电网日指标」读的是 GD_METRIC_URL 指向的 CSV，
那份文件就是这个接口的导出（已逐点核对：同一 日期/时间/typeId 的数值与原文件完全一致，
2026-08-31 三类共 288 行字符串级 0 mismatch）。上游停更时，用本脚本把缺口补齐。

输出列与 gd_metric.csv 一致：时间戳,日期,时间,typeId,类型,单位,数值,sourceId
  typeId 21 日前电价 / 22 实时电价（元/兆瓦时）、23 负荷（MW）
  sourceId 9  = 广东电力交易中心（实际值）
  sourceId 999 = 电查查（预测值）

用法
----
  export DIANCHACHA_BASE_URL=https://<host>
  cp creds.json.example creds.json      # 填 authorization / jwt / cookie / aesKey / aesIv
  python3 fetch_gd_metric.py --from 2026-09-01 --to 2026-10-06 --out patch.csv
  python3 fetch_gd_metric.py --month 2026-09 --append-to gd_metric.csv --out gd_metric.csv
  python3 fetch_gd_metric.py --from 2026-09-01 --to 2026-10-06 --source 9 --out only_actual.csv

要点
----
* 一次请求取一整段区间（不要逐天发）：接口单次就能返回整月，
  实测补 09-01~10-06 三类只需 3 个请求，逐天发反而会被风控 403。
* 凭证只活十来分钟 / 十几个请求：凭证失效（401/403）立刻停手，不做任何重试。
* 先调免登录的 /home/page/electricityPricesLatest 看服务端最新到哪天，避免白跑。
"""
import argparse
import base64
import calendar
import csv
import datetime as dt
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request

# 站点地址与前端解密参数都不写入仓库：
#   DIANCHACHA_BASE_URL  必填，形如 https://<host>
#   DIANCHACHA_AES_KEY / DIANCHACHA_AES_IV  可选；也可写在 creds.json 的 aesKey / aesIv
BASE = os.environ.get("DIANCHACHA_BASE_URL", "").strip().rstrip("/")
SPOT = BASE + "/electric/api/v2/trade/power/data/spot"
LATEST = BASE + "/electric/api/v2/home/page/electricityPricesLatest"
REFERER = BASE + "/transaction-new/trade-data"
UA = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36 Edg/139.0.0.0")

# 前端 Decrypt 用的 AES-128-CBC / PKCS7 参数从 creds.json 或环境变量取。

COLUMNS = ["时间戳", "日期", "时间", "typeId", "类型", "单位", "数值", "sourceId"]
TYPES = {"21": "日前电价", "22": "实时电价", "23": "负荷"}
DEFAULT_TYPES = ["21", "22", "23"]
CHUNK_DAYS = 31          # 单次请求最多覆盖的天数
DELAY_SECONDS = 3.0      # 请求间隔，避免触发风控
TIMEOUT = 60
MAX_TRANSIENT_RETRIES = 2   # 只对 5xx / 网络错误重试；4xx 直接放弃


class AuthError(RuntimeError):
    """凭证失效（401/403）——不要重试，换 creds.json 再来。"""


# --------------------------------------------------------------------------- 解密

def aes_params(creds_):
    key = (creds_.get("aesKey") or os.environ.get("DIANCHACHA_AES_KEY") or "").encode()
    iv = (creds_.get("aesIv") or os.environ.get("DIANCHACHA_AES_IV") or "").encode()
    if len(key) != 16 or len(iv) != 16:
        sys.exit("缺少 AES 参数：在 creds.json 填 aesKey/aesIv（各 16 字节），"
                 "或设置 DIANCHACHA_AES_KEY / DIANCHACHA_AES_IV")
    return key, iv


def decrypt(ct_b64, creds_):
    """AES-128-CBC + PKCS7，入参是 base64 密文，返回明文字节。"""
    key, iv = aes_params(creds_)
    raw = base64.b64decode(ct_b64)
    for decryptor in (_decrypt_cryptography, _decrypt_openssl):
        try:
            return decryptor(raw, key, iv)
        except ImportError:
            continue
    raise RuntimeError("既没有 cryptography 也没有 openssl，无法解密")


def _decrypt_cryptography(raw, key, iv):
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    from cryptography.hazmat.primitives.padding import PKCS7

    dec = Cipher(algorithms.AES(key), modes.CBC(iv)).decryptor()
    padded = dec.update(raw) + dec.finalize()
    unpadder = PKCS7(128).unpadder()
    return unpadder.update(padded) + unpadder.finalize()


def _decrypt_openssl(raw, key, iv):
    proc = subprocess.run(
        ["openssl", "enc", "-d", "-aes-128-cbc", "-nopad",
         "-K", key.hex(), "-iv", iv.hex()],
        input=raw, capture_output=True, check=True)
    padding = proc.stdout[-1]
    if not 1 <= padding <= 16 or proc.stdout[-padding:] != bytes([padding]) * padding:
        raise RuntimeError("openssl 解密结果 padding 不对")
    return proc.stdout[:-padding]


# --------------------------------------------------------------------------- HTTP

def creds():
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "creds.json")
    if not os.path.exists(path):
        sys.exit("缺少 creds.json —— 先 cp creds.json.example creds.json 并填入凭证")
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def request(path, creds_=None, formatter=None):
    """GET 一次；401/403 抛 AuthError，其他错误少量重试。"""
    headers = {"Accept": "application/json, text/plain, */*", "User-Agent": UA}
    if creds_:
        headers.update({
            "Authorization": creds_.get("authorization", ""),
            "JWTHeaderName": creds_.get("jwt", ""),
            "Cookie": creds_.get("cookie", ""),
            "Referer": REFERER,
        })
    req = urllib.request.Request(BASE + path, headers=headers)
    for attempt in range(MAX_TRANSIENT_RETRIES + 1):
        try:
            with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
                return resp.read()
        except urllib.error.HTTPError as e:
            if e.code in (401, 403):
                raise AuthError(
                    f"凭证失效（HTTP {e.code}）——电查查的 token 只活十来分钟，"
                    f"换 creds.json 里的 jwt/authorization 后再跑") from e
            if attempt == MAX_TRANSIENT_RETRIES:
                raise
        except (urllib.error.URLError, TimeoutError):
            if attempt == MAX_TRANSIENT_RETRIES:
                raise
        time.sleep(DELAY_SECONDS * (attempt + 1))
    raise RuntimeError("unreachable")


def per_type_latest(region, type_id):
    """免登录接口：服务端该类型最新到哪个时刻（'YYYY-MM-DD HH:MM:SS'）。"""
    raw = request(f"/electric/api/v2/home/page/electricityPricesLatest?typeIds={type_id}")
    for item in json.loads(raw).get("data") or []:
        if str(item.get("regionId")) == str(region):
            return item.get("latestTime")
    return None


def fetch_spot(creds_, region, type_id, start, end):
    """取 [start, end] 区间该类型的全部点（含 sourceId 999 预测）。"""
    query = (f"?&regionId={region}&typeId={type_id}&timeInterval=15"
             f"&start={start}&end={end}&watershed={end}")
    body = json.loads(request(SPOT + query, creds_))
    if body.get("status") != 200:
        raise RuntimeError(f"接口返回异常 - status={body.get('status')} message={body.get('message')}")
    payload = body.get("data")
    if not payload:
        return []
    return json.loads(decrypt(payload, creds_)) or []


# --------------------------------------------------------------------------- 组装

def stamp(day, clock):
    """日期 + 时间 → 报告时刻。24:00 归到次日 00:00:00。"""
    date = dt.datetime.strptime(day, "%Y-%m-%d")
    if clock == "24:00":
        date += dt.timedelta(days=1)
        clock = "00:00"
    return f"{date:%Y-%m-%d} {clock}:00"


def collect(creds_, region, type_id, chunks, delay):
    """按区间分块抓取，返回 {(日期,时间,typeId): row}；同一 key 优先保留实际值 sourceId=9。"""
    picked = {}
    for start, end in chunks:
        payload = fetch_spot(creds_, region, type_id, start, end)
        for item in payload.get("values") or []:
            clock = str(item.get("time") or "")
            if not re.match(r"^\d{2}:\d{2}$", clock):
                continue      # 丢掉「平均值」等汇总行
            key = (item["day"], clock, type_id)
            prev = picked.get(key)
            if prev is None or (str(item["sourceId"]) == "9" and prev["sourceId"] != "9"):
                picked[key] = {
                    "时间戳": stamp(item["day"], clock),
                    "日期": item["day"],
                    "时间": clock,
                    "typeId": type_id,
                    "类型": TYPES.get(type_id, ""),
                    "单位": item.get("dataUnit") or "",
                    "数值": str(item.get("dataValue")),
                    "sourceId": str(item.get("sourceId", "")),
                }
        time.sleep(delay)
    return picked


def month_chunks(year, month):
    last = calendar.monthrange(year, month)[1]
    return [(f"{year:04d}{month:02d}01" + "000000",
             f"{year:04d}{month:02d}{last:02d}" + "235959")]


def range_chunks(start_date, end_date):
    """把 [start_date, end_date] 切成每块最多 CHUNK_DAYS 天。"""
    chunks = []
    cursor, last = start_date, end_date
    while cursor <= last:
        stop = min(cursor + dt.timedelta(days=CHUNK_DAYS - 1), last)
        chunks.append((f"{cursor:%Y%m%d}000000", f"{stop:%Y%m%d}235959"))
        cursor = stop + dt.timedelta(days=1)
    return chunks


def read_existing(path):
    with open(path, encoding="utf-8-sig", newline="") as f:
        return list(csv.DictReader(f))


def write_csv(path, rows, with_bom):
    encoding = "utf-8-sig" if with_bom else "utf-8"
    with open(path, "w", encoding=encoding, newline="") as f:
        writer = csv.DictWriter(f, fieldnames=COLUMNS, lineterminator="\n")
        writer.writeheader()
        writer.writerows(rows)


# --------------------------------------------------------------------------- main

def main():
    parser = argparse.ArgumentParser(description="电查查现货数据 → gd_metric.csv 格式")
    parser.add_argument("--region", default="440000", help="国标 adcode，默认 440000 广东")
    parser.add_argument("--types", default=",".join(DEFAULT_TYPES))
    parser.add_argument("--from", dest="date_from", help="起始日期 YYYY-MM-DD")
    parser.add_argument("--to", dest="date_to", help="结束日期 YYYY-MM-DD")
    parser.add_argument("--month", help="整月 YYYY-MM（等价于 --from/--to 覆盖该月）")
    parser.add_argument("--out", help="输出 CSV（--check 时不需要）")
    parser.add_argument("--append-to", help="已有 CSV，跳过其中已存在的 (日期,时间,typeId) 后合并输出")
    parser.add_argument("--source", help="只保留该 sourceId，如 9（只要实际值）")
    parser.add_argument("--delay", type=float, default=DELAY_SECONDS, help="请求间隔秒")
    parser.add_argument("--no-bom", action="store_true", help="不写 UTF-8 BOM")
    parser.add_argument("--check", action="store_true", help="只看服务端最新到哪天，不抓数据")
    args = parser.parse_args()

    if not BASE:
        sys.exit("缺少 DIANCHACHA_BASE_URL —— 站点地址不内置，请先导出环境变量")

    types = [t.strip() for t in args.types.split(",") if t.strip()]
    for t in types:
        if t not in TYPES:
            sys.exit(f"未知 typeId: {t}（可用 {', '.join(TYPES)}）")

    if args.month:
        match = re.match(r"^(\d{4})-(\d{2})$", args.month)
        if not match:
            sys.exit("--month 需为 YYYY-MM")
        year, month = int(match.group(1)), int(match.group(2))
        chunks = month_chunks(year, month)
    elif args.date_from and args.date_to:
        chunks = range_chunks(dt.date.fromisoformat(args.date_from),
                              dt.date.fromisoformat(args.date_to))
    else:
        chunks = None

    print("服务端最新数据：")
    for t in types:
        print(f"  type {t} {TYPES[t]}: {per_type_latest(args.region, t)}")
    if args.check:
        return
    if chunks is None:
        sys.exit("需要 --month YYYY-MM，或 --from/--to YYYY-MM-DD")
    if not args.out:
        sys.exit("需要 --out 指定输出文件")
    if not args.month and args.date_to and all(
            (per_type_latest(args.region, t) or "") < args.date_to for t in types):
        print("提示：请求区间的末尾晚于服务端已有数据，末尾只会拿到预测值（sourceId 999）")

    rows_by_key = {}
    if args.append_to:
        rows_by_key = {(r["日期"], r["时间"], r["typeId"]): r for r in read_existing(args.append_to)}
        print(f"已有 {len(rows_by_key)} 行，将跳过其中已存在的点")

    creds_ = creds()
    for type_id in types:
        picked = collect(creds_, args.region, type_id, chunks, args.delay)
        added = 0
        for key3, row in picked.items():
            if key3 in rows_by_key:
                continue
            rows_by_key[key3] = row
            added += 1
        print(f"type {type_id} {TYPES[type_id]}: 服务端 {len(picked)} 点，新增 {added} 行")

    rows = list(rows_by_key.values())
    if args.source:
        rows = [r for r in rows if r["sourceId"] == args.source]
    rows.sort(key=lambda r: (r["时间戳"], r["typeId"]))
    write_csv(args.out, rows, with_bom=not args.no_bom)
    print(f"写出 {args.out}：{len(rows)} 行")


if __name__ == "__main__":
    try:
        main()
    except AuthError as e:
        sys.exit(str(e))
