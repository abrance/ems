package spotreport

import "testing"

// 空数组必须序列化成 []，不能是 null：接口返回值里 null 会让下游判空写法各写一套
func TestNonNilStrings(t *testing.T) {
	if got := nonNilStrings(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil 应转为空切片，实际 %#v", got)
	}
	in := []string{"a"}
	if got := nonNilStrings(in); len(got) != 1 || got[0] != "a" {
		t.Fatalf("非空切片不应被改动：%#v", got)
	}
	if got := nonNilStrings([]string{}); got == nil {
		t.Fatalf("空切片应保持非 nil")
	}
}
