package auth

import "testing"

// 这条用例是冲着「普通用户改掉管理员密码」那个提权漏洞写的：
// 当时目标账号直接取自请求体且不做任何校验，所以 (user, admin) 会放行。
func TestResolveChangeTarget(t *testing.T) {
	cases := []struct {
		name      string
		current   string
		requested string
		want      string
		wantErr   bool
	}{
		{"普通用户改自己", "user", "user", "user", false},
		{"普通用户留空即改自己", "user", "", "user", false},
		{"普通用户改管理员 → 必须拒绝", "user", "admin", "", true},
		{"普通用户大写也算自己", "user", " USER ", "user", false},
		{"管理员改自己", "admin", "admin", "admin", false},
		{"管理员留空即改自己", "admin", "", "admin", false},
		{"管理员重置普通用户", "admin", "user", "user", false},
		{"非法目标", "admin", "root", "", true},
		{"会话缺失", "", "user", "", true},
	}
	for _, c := range cases {
		got, err := resolveChangeTarget(c.current, c.requested)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: 期望拒绝，实际放行到 %q", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: 期望放行，实际报错 %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: 目标 = %q，期望 %q", c.name, got, c.want)
		}
	}
}
