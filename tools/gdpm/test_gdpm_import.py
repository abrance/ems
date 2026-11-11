#!/usr/bin/env python3
"""gdpm_import 的最小单测：指标映射与写入行格式。

只跑纯函数，不联网。  python3 test_gdpm_import.py
"""

import unittest

import gdpm_import as gi


class TestMapping(unittest.TestCase):
    def test_market(self):
        self.assertEqual(gi.market_of("现货市场总体运行情况（日前）"), "dayahead")
        self.assertEqual(gi.market_of("现货市场总体运行情况（实时）"), "realtime")
        self.assertIsNone(gi.market_of("中长期市场月度交易情况"))
        self.assertIsNone(gi.market_of("现货市场总体运行情况"))

    def test_series_of(self):
        self.assertEqual(
            gi.series_of("generationSideDailyAvgPrice", "dayahead"),
            {"metric": "gdpm_price_cny_mwh",
             "labels": {"market": "dayahead", "side": "gen", "kind": "weighted_avg",
                        "src": "pm.gd.csg.cn"}},
        )
        self.assertEqual(gi.series_of("consumerSideDailyTotalVolume", "dayahead")["labels"]["side"], "load")
        self.assertEqual(gi.series_of("generationSideDailyMaxPrice", "dayahead")["labels"]["kind"], "max")
        self.assertEqual(gi.series_of("generationSideDailyMinPrice", "dayahead")["labels"]["kind"], "min")
        self.assertEqual(gi.series_of("dailyDeclarationAvgPrice", "dayahead")["labels"]["kind"], "declared_avg")
        self.assertEqual(gi.series_of("generationSideRealTimeAvgPrice", "realtime")["labels"]["kind"],
                         "weighted_avg")
        self.assertIsNone(gi.series_of("date", "dayahead"))

    def test_day_ts_ms(self):
        # 2026-11-10 00:00:00+08:00
        self.assertEqual(gi.day_ts_ms("2026-11-10"), 1794240000000)


class TestRowsToSamples(unittest.TestCase):
    # 真实回包的裁剪版：实时表里的行会夹带日前字段，必须按本表声明字段过滤掉
    RESULTS = [{
        "templateName": "现货市场总体运行情况（日前）",
        "dateField": "date",
        "fields": [["date", "日期"], ["generationSideDailyTotalVolume", "电量"],
                   ["generationSideDailyAvgPrice", "均价"]],
        "rows": [{"date": "2026-11-10", "generationSideDailyTotalVolume": 1540786.335,
                  "generationSideDailyAvgPrice": 334.569613,
                  "generationSideRealTimeAvgPrice": 999.0}],
    }]

    def test_lines(self):
        lines, skipped = gi.rows_to_samples(self.RESULTS)
        self.assertEqual(skipped, [])
        self.assertEqual(len(lines), 2)
        self.assertIn(
            'gdpm_price_cny_mwh{kind="weighted_avg",market="dayahead",side="gen",src="pm.gd.csg.cn"} '
            '334.569613 1794240000000', lines)
        self.assertIn(
            'gdpm_volume_mwh{kind="total",market="dayahead",side="gen",src="pm.gd.csg.cn"} '
            '1540786.335 1794240000000', lines)
        # 未声明的实时字段没有被带出来
        self.assertFalse(any("999" in line for line in lines))

    def test_unknown_template_skipped(self):
        lines, skipped = gi.rows_to_samples([{
            "templateName": "中长期市场交易情况",
            "dateField": "date",
            "fields": [["x", "y"]],
            "rows": [{"date": "2026-11-10", "x": 1}],
        }])
        self.assertEqual(lines, [])
        self.assertEqual(len(skipped), 1)


if __name__ == "__main__":
    unittest.main(verbosity=2)
