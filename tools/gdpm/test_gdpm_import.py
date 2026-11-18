#!/usr/bin/env python3
"""gdpm_import 的最小单测：指标映射与写入行格式。

只跑纯函数，不联网。  python3 test_gdpm_import.py
"""

import json
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
                        "src": "official"}},
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
            'gdpm_price_cny_mwh{kind="weighted_avg",market="dayahead",side="gen",src="official"} '
            '334.569613 1794240000000', lines)
        self.assertIn(
            'gdpm_volume_mwh{kind="total",market="dayahead",side="gen",src="official"} '
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


class VerifyExportTest(unittest.TestCase):
    """--verify 的读路径：export 只认当天的真实样本，不能拿邻近日期的值顶上。"""

    DAY = "2026-11-10"
    DAY_MS = 1794240000000

    def body(self, *samples):
        rows = [{"metric": {"__name__": "gdpm_price_cny_mwh"},
                 "values": [v for v, _ in samples],
                 "timestamps": [t for _, t in samples]}]
        return "\n".join(json.dumps(r) for r in rows) + "\n"

    def test_returns_value_of_that_day(self):
        body = self.body((334.569613, self.DAY_MS))
        self.assertEqual(gi.parse_export_day(body, self.DAY), 334.569613)

    def test_empty_body_means_no_data(self):
        self.assertIsNone(gi.parse_export_day("", self.DAY))

    def test_previous_day_value_not_used(self):
        # 前一天的样本落在窗口外：必须当成「当天无数据」
        body = self.body((350.564512, self.DAY_MS - 86400000))
        self.assertIsNone(gi.parse_export_day(body, self.DAY))

    def test_error_line_raises(self):
        body = json.dumps({"status": "error", "error": "bad_data"}) + "\n"
        with self.assertRaises(RuntimeError):
            gi.parse_export_day(body, self.DAY)


if __name__ == "__main__":
    unittest.main(verbosity=2)
