package v1

import "github.com/gogf/gf/v2/frame/g"

type LogoutReq struct {
	g.Meta `path:"/auth/logout" method:"post" tags:"认证" summary:"用户退出" role:"user"`
}
type LogoutRes struct{}

type ChangePasswordReq struct {
	g.Meta `path:"/auth/change-password" method:"post" tags:"认证" summary:"修改密码" role:"user"`
	// Role 是**要修改哪个账号**，不是「你是谁」。
	// 留空 = 改当前会话自己的密码；管理员可指定 user 去重置普通用户密码；
	// 指定 admin 只允许在「当前会话就是 admin」时（即改自己）。
	Role string `json:"role" dc:"目标账号：admin / user；留空=改当前会话自己的"`
	// OldPassword 是**当前会话角色本人**的密码，用来证明是你本人在操作。
	// 即管理员替普通用户重置密码时，填的也是管理员自己的密码。
	OldPassword string `json:"oldPassword" v:"required#请输入当前密码"`
	NewPassword string `json:"newPassword" v:"required#新密码不能为空"`
}
type ChangePasswordRes struct{}

type GetCurrentUserReq struct {
	g.Meta `path:"/auth/current" method:"get" tags:"认证" summary:"获取当前用户信息"`
}
type GetCurrentUserRes struct {
	Role string `json:"role"`
}
