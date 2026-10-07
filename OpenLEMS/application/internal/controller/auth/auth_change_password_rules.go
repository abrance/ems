package auth

import (
	"common/c_enum"
	"strings"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// resolveChangeTarget 决定这次改密的**目标账号**，并做权限判定。
//
// 这里是修一个提权漏洞的地方，改动前请先看完这段：
//
// 原先的实现直接拿请求体里的 role 当目标账号，没有任何校验，而接口只要求
// role:"user"（普通用户即可调用）。于是**任何已登录的普通用户**都能发一个
// `{role:"admin", newPassword:"..."}` 把管理员密码改掉，然后以管理员身份登录。
//
// 规则（改自己 / 管理员重置普通用户，除此之外都不放行）：
//
//	目标为空            → 当前会话角色（改自己）
//	目标 == 当前角色     → 放行
//	当前角色不是 admin   → 拒绝（普通用户只能改自己的）
//	目标 == admin       → 拒绝（管理员也不能改别人的 admin —— 那不是自己，
//	                      而他改自己的那条路上面已经放行了，没有合法用途）
//	其余（admin 改 user）→ 放行
func resolveChangeTarget(current, requested string) (string, error) {
	current = normalizeRole(current)
	requested = normalizeRole(requested)
	if current == "" {
		return "", gerror.NewCode(gcode.CodeNotAuthorized, "会话无效，请重新登录")
	}
	if requested == "" {
		requested = current
	}
	if !validRole(requested) {
		return "", gerror.NewCode(gcode.CodeInvalidParameter, "角色必须是 user 或 admin")
	}
	if requested == current {
		return requested, nil
	}
	// 下面两条判断有重叠（只有两个角色时，前者能覆盖后者），**故意如此**：
	// 安全判定里宁可多一条显式规则，也不依赖「角色只有两个」这个当前事实。
	if current != string(c_enum.EUserRoleAdmin) {
		return "", gerror.NewCode(gcode.CodeNotAuthorized, "只能修改自己的密码")
	}
	if requested == string(c_enum.EUserRoleAdmin) {
		return "", gerror.NewCode(gcode.CodeNotAuthorized, "不能修改其他管理员的密码")
	}
	return requested, nil
}

func normalizeRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

func validRole(role string) bool {
	return role == string(c_enum.EUserRoleAdmin) || role == string(c_enum.EUserRoleUser)
}
