package cmd

import (
	"strings"
	"testing"
)

func TestAuthBypassScript(t *testing.T) {
	script := authBypassScript("admin")
	for _, want := range []string{"ems:user:token", "ems:user:role", `"admin"`, "ems:user:expireAt"} {
		if !strings.Contains(script, want) {
			t.Fatalf("注入脚本缺少 %q: %s", want, script)
		}
	}
	if !strings.HasPrefix(script, "<script>") || !strings.HasSuffix(script, "</script>") {
		t.Fatalf("注入脚本必须是完整 script 标签: %s", script)
	}

	// 角色值即使被篡改也不能提前闭合 script 标签（防止注入）
	evil := authBypassScript(`x</script><script>alert(1)</script>`)
	if strings.Contains(evil, "</script><script>alert") {
		t.Fatalf("角色值未做转义，存在注入风险: %s", evil)
	}
}
