package internal

import "testing"

// 写入 payload 的格式被钉死：改格式就得改这个测试，避免线上悄悄写坏。
func TestFormatSamples(t *testing.T) {
	got := formatSamples([]sample{
		{
			Name: "gdpm_price_cny_mwh",
			Labels: map[string]string{
				"src":    "official",
				"kind":   "weighted_avg",
				"market": "dayahead",
				"side":   "gen",
			},
			Value: 334.569613,
			TS:    1762704000000,
		},
		{
			Name:   MetricNameEmsMetric,
			Labels: map[string]string{LabelNameType: "system", LabelNameID: "cpu", LabelNameField: "usage"},
			Value:  12.5,
			TS:     1762704060000,
		},
	})

	want := "gdpm_price_cny_mwh{kind=\"weighted_avg\",market=\"dayahead\",side=\"gen\",src=\"official\"} 334.569613 1762704000000\n" +
		"ems_metric{field=\"usage\",id=\"cpu\",type=\"system\"} 12.5 1762704060000\n"
	if got != want {
		t.Fatalf("payload 与约定不一致\n got: %q\nwant: %q", got, want)
	}
}

func TestEscapeLabelValue(t *testing.T) {
	cases := map[string]string{
		"plain":        "plain",
		`a"b`:          `a\"b`,
		`a\b`:          `a\\b`,
		"a\nb":         `a\nb`,
		"official": "official",
	}
	for in, want := range cases {
		if got := escapeLabelValue(in); got != want {
			t.Errorf("escapeLabelValue(%q) = %q, want %q", in, got, want)
		}
	}
}
