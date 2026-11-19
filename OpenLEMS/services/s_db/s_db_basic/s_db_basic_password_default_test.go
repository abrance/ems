package s_db_basic

import (
	"testing"

	"common/c_util"
)

// 初始密码的契约测试。
//
// 为什么值得测：初始密码同时被登录页提示、部署文档和运维口头约定引用，
// 但它在代码里只是一个字符串常量 —— 谁都能顺手改掉，而改坏了不会有任何编译或运行时错误，
// 只会让人登不进去（或更糟：让人以为密码是 admin 而其实是别的）。
//
// 链条：常量 DefaultValue → init 里被 IsPasswordSettingID 认出并 bcrypt 哈希 → 登录时 VerifyPassword 比对。
// 这里把这三段串起来断言，缺任何一段都登不进去。
func TestInitialPasswordDefaults(t *testing.T) {
	// want 是**对外承诺**的初始密码，不是从常量抄一遍：
	// 改这两处必须同时改登录页提示与部署文档，否则运维会照着错的说明试。
	cases := []struct {
		def  *SSystemSettingDefine
		want string
	}{
		{SystemSettingAdminPassword, "admin"},
		{SystemSettingUserPassword, "111111"},
	}

	for _, c := range cases {
		t.Run(c.def.Id, func(t *testing.T) {
			if c.def.DefaultValue == "" {
				// VerifyPassword 对空串一律返回 false，空默认值 = 谁都进不来
				t.Fatal("默认密码为空，任何人都无法登录")
			}
			if !IsPasswordSettingID(c.def.Id) {
				t.Fatal("该设置没被 IsPasswordSettingID 覆盖，init 不会哈希它，密码会以明文入库")
			}
			if c.def.DefaultValue != c.want {
				t.Errorf("默认密码 = %q，对外承诺的是 %q；改了就要同步登录页提示与部署文档", c.def.DefaultValue, c.want)
			}

			// 走一遍 init 与登录实际走的路径
			hashed, err := c_util.HashPassword(c.def.DefaultValue)
			if err != nil {
				t.Fatalf("哈希初始密码失败: %v", err)
			}
			ok, err := c_util.VerifyPassword(hashed, c.want)
			if err != nil {
				t.Fatalf("校验初始密码失败: %v", err)
			}
			if !ok {
				t.Errorf("哈希后的初始密码用 %q 校验不过，登录会失败", c.want)
			}
		})
	}
}

// 初始口令可以被部署环境的 EMS_ADMIN_PASSWORD / EMS_USER_PASSWORD 覆盖。
// 只覆盖「建库时写什么」，不改已有库里的值 —— 这里只断言取值函数，
// 建库时的使用点在 s_db_init_f.go（InitialPassword）。
func TestInitialPasswordEnvOverride(t *testing.T) {
	t.Setenv(EnvAdminPassword, "  s3cret-admin  ")
	if got := InitialPassword(SystemSettingAdminPassword); got != "s3cret-admin" {
		t.Errorf("admin 初始口令 = %q，应为环境变量值（去空白）", got)
	}

	t.Setenv(EnvUserPassword, "")
	if got := InitialPassword(SystemSettingUserPassword); got != "111111" {
		t.Errorf("user 初始口令 = %q，环境变量为空时应回落到出厂默认值", got)
	}

	// 非密码设置不该被这两个环境变量影响
	t.Setenv(EnvAdminPassword, "nope")
	if got := InitialPassword(SystemSettingPasswordLength); got != "6" {
		t.Errorf("非密码设置的默认值被误改 = %q", got)
	}
}
