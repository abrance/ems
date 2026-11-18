#!/usr/bin/env python3
"""广东电力交易中心指标 -> 可读 CSV 数据集（宽表 + 长表 + 原始 JSON）。

抓取复用 `gd_pm_fetch.fetch_template()`（同一套端点/索引/限速），这里只负责
把索引里的每个模板整表拉下来，落成能直接用 Excel 打开的文件。

产物（默认 data/gdpm/）：

    raw/<templateUuid>.json          每个模板的原始返回（含字段定义，事后可追溯）
    spot_day_ahead_<年>.csv          日前日频宽表，一天一行，列=源站中文列名
    spot_realtime_<年>.csv           实时日频宽表，同上
    metrics_<slug>.csv               月频/年频/静态模板，一个模板一个文件
    gdpm_long.csv                    日频长表 date,dataset,scope,metric,value,unit,source

单位统一：电价 `元/兆瓦时`，电量 `MWh`（源站表头写的是 元/MWh，这里归一）。

用法：
  python3 gdpm_dataset.py                       # 拉到 data/gdpm/
  python3 gdpm_dataset.py --out /tmp/gdpm       # 指定目录
  python3 gdpm_dataset.py --only 现货            # 只拉名字含「现货」的模板（调试用）
"""

import argparse
import csv
import json
import os
import re
import sys

import gd_pm_fetch as fetch

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_OUT = os.path.normpath(os.path.join(HERE, "..", "..", "data", "gdpm"))
SRC = fetch.SOURCE_LABEL

# 日频模板 -> (文件名前缀, dataset, 涨跌口径)
DAILY = {
    "现货市场总体运行情况（日前）": ("spot_day_ahead", "day_ahead_trade"),
    "现货市场总体运行情况（实时）": ("spot_realtime", "realtime_trade"),
}

# 非日频模板 -> 文件名 slug（认不出就用模板名净化后代替）
SLUG = {
    "中长期年度交易成交均价及电量等总体情况": "mid_long_term_year",
    "中长期月度交易成交均价及电量等总体情况": "mid_long_term_month",
    "绿电年度交易成交均价及电量等总体情况": "green_power_year",
    "绿电月度交易成交均价及电量等总体情况": "green_power_month",
    "绿电零售交易成交电量、均价及用户数": "green_power_retail",
    "电能量零售交易成交电量、均价及用户数": "energy_retail",
    "HHI指标": "hhi",
    "Top-m指标": "top_m",
    "电网企业基本信息": "grid_company",
    "电网企业关联信息": "grid_company_related",
    "交易机构基本信息": "exchange_company",
    "交易中心股权结构": "exchange_shareholders",
}

UNIT_BY_FIELD = (
    ("Volume", "MWh"),
    ("Price", "元/兆瓦时"),
)

# 主体名单类披露（退市售电公司/电力用户）不是交易指标：售电公司 8.5k 行、电力用户
# 78 万行，拉下来既不是时序也用不上，明确跳过并在 README 写明。
SKIP_NAME = ("名单",)
MAX_ROWS = 20000


def unit_of(field_key, label):
    """列单位：先按字段名前缀，再退回表头括号里的单位（统一成 元/兆瓦时）。"""
    for needle, unit in UNIT_BY_FIELD:
        if needle in field_key:
            return unit
    m = re.search(r"（(.+?)）$", label or "")
    if not m:
        return ""
    unit = m.group(1).strip()
    return "元/兆瓦时" if unit in ("元/MWh", "元/兆瓦时") else unit


def scope_of(field_key, label):
    if field_key.startswith("consumerSide"):
        return "用电侧"
    if field_key.startswith("generationSide"):
        return "发电侧"
    if "申报" in (label or ""):
        return "发电侧"
    return ""


def metric_name(label):
    """去掉表头尾部的单位括号，得到指标名。"""
    return re.sub(r"（[^（）]*）\s*$", "", label or "").strip()


def write_csv(path, header, rows):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    # utf-8-sig：Excel 直接双击打开不乱码
    with open(path, "w", newline="", encoding="utf-8-sig") as f:
        w = csv.writer(f)
        w.writerow(header)
        w.writerows(rows)
    return path


def dedupe_templates(index):
    """同一 templateUuid 挂在多个节点上时只算一份。"""
    seen, out = set(), []
    for entries in index.values():
        for e in entries:
            tpl = e.get("templateUuid")
            if not tpl or tpl in seen:
                continue
            seen.add(tpl)
            out.append(e)
    return out


def dump(out_dir, only=None, log=print):
    index = fetch.load_index()
    templates = dedupe_templates(index)
    if only:
        templates = [e for e in templates
                     if only.lower() in (e.get("templateName") or "").lower()]
    log(f"待拉取模板 {len(templates)} 个")
    budget = fetch.Budget(400)
    long_rows, written = [], []

    for i, e in enumerate(templates, 1):
        name = e.get("templateName") or e["templateUuid"]
        if any(p in name for p in SKIP_NAME):
            log(f"  [{i}/{len(templates)}] {name}: 跳过（主体名单类，非指标）")
            continue
        if (e.get("total") or 0) > MAX_ROWS:
            log(f"  [{i}/{len(templates)}] {name}: 跳过（{e['total']} 行，超上限 {MAX_ROWS}）")
            continue
        try:
            rows = fetch.fetch_template(e, budget)
        except fetch.BudgetExceeded:
            log(f"  [{i}/{len(templates)}] {name}: 请求预算用尽，后续模板未拉")
            break
        labels = [f[1] for f in e.get("fields") or []]
        keys = [f[0] for f in e.get("fields") or []]
        raw_path = os.path.join(out_dir, "raw", f"{e['templateUuid']}.json")
        os.makedirs(os.path.dirname(raw_path), exist_ok=True)
        with open(raw_path, "w", encoding="utf-8") as f:
            json.dump({"templateUuid": e["templateUuid"], "templateName": name,
                       "nodes": e.get("nodes"), "dateField": e.get("dateField"),
                       "fields": e.get("fields"), "total": len(rows), "rows": rows},
                      f, ensure_ascii=False, indent=1)

        date_field = e.get("dateField")
        if name in DAILY and date_field:
            prefix, dataset = DAILY[name]
            by_year = {}
            for r in rows:
                day = str(r.get(date_field, ""))[:10]
                by_year.setdefault(day[:4], []).append(r)
            for year, year_rows in sorted(by_year.items()):
                year_rows.sort(key=lambda r: str(r.get(date_field, ""))[:10], reverse=True)
                path = write_csv(os.path.join(out_dir, f"{prefix}_{year}.csv"),
                                 labels, [[r.get(k, "") for k in keys] for r in year_rows])
                written.append((path, len(year_rows)))
            for r in rows:
                day = str(r.get(date_field, ""))[:10]
                for k, label in zip(keys, labels):
                    if k == date_field:
                        continue
                    raw = r.get(k)
                    if raw in (None, ""):
                        continue
                    long_rows.append([day, dataset, scope_of(k, label),
                                      metric_name(label), raw, unit_of(k, label), SRC])
        else:
            slug = SLUG.get(name) or re.sub(r"[^0-9a-zA-Z一-鿿]+", "_", name).strip("_")
            path = write_csv(os.path.join(out_dir, f"metrics_{slug}.csv"),
                             labels, [[r.get(k, "") for k in keys] for r in rows])
            written.append((path, len(rows)))
        log(f"  [{i}/{len(templates)}] {name}: {len(rows)} 行（请求累计 {budget.used}）")

    long_rows.sort(key=lambda r: (r[0], r[1], r[2], r[3]), reverse=True)
    path = write_csv(os.path.join(out_dir, "gdpm_long.csv"),
                     ["date", "dataset", "scope", "metric", "value", "unit", "source"],
                     long_rows)
    written.append((path, len(long_rows)))
    return written, budget.used


def main():
    ap = argparse.ArgumentParser(description="广东电力交易指标 -> CSV 数据集")
    ap.add_argument("--out", default=DEFAULT_OUT, help=f"输出目录（默认 {DEFAULT_OUT}）")
    ap.add_argument("--only", help="只拉名字含该子串的模板")
    args = ap.parse_args()
    written, used = dump(args.out, only=args.only, log=lambda m: print(m, file=sys.stderr))
    for path, n in written:
        print(f"{n:6d}  {path}")
    print(f"请求 {used} 次")
    return 0


if __name__ == "__main__":
    sys.exit(main())
