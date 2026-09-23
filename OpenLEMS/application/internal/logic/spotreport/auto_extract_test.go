package spotreport

import "testing"

func TestAutoExtractEnabled(t *testing.T) {
	cases := map[string]bool{
		"":      true, // 默认开启
		"on":    true,
		"1":     true,
		"off":   false,
		"false": false,
		"0":     false,
		"NO":    false,
		" off ": false,
	}
	for value, want := range cases {
		t.Setenv(EnvAutoExtract, value)
		if got := autoExtractEnabled(); got != want {
			t.Fatalf("%s=%q 期望 %v，实际 %v", EnvAutoExtract, value, want, got)
		}
	}
}
