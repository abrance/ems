package auth

import (
	v1 "application/api/auth/v1"
	"common/c_enum"
	"common/c_log"
	"common/c_util"
	"context"
	"s_db"
	"s_db/s_db_basic"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

// ChangePassword 修改密码。
//
// 两条约束（缺一不可，见 auth_change_password_rules.go 的 resolveChangeTarget）：
//
//  1. **目标账号不能由请求方随便指定**：只能改自己的，或管理员重置普通用户。
//     放任请求体里的 role 决定目标，等于把管理员密码交给任何已登录的普通用户。
//  2. **必须先证明是本人操作**：校验当前会话角色自己的密码（oldPassword）。
//     原先没有这一步，任意一个没锁屏的会话就能把密码改掉。
//
// 目标账号由 resolveChangeTarget 判定，这里只负责校验旧密码与落库。
func (c *Controller) ChangePassword(ctx context.Context, req *v1.ChangePasswordReq) (res *v1.ChangePasswordRes, err error) {
	r := g.RequestFromCtx(ctx)
	if r == nil {
		return nil, gerror.NewCode(gcode.CodeInternalError, "请求上下文无效")
	}

	currentRole := r.Session.MustGet("role").String()
	target, err := resolveChangeTarget(currentRole, req.Role)
	if err != nil {
		c_log.BizWarningf(ctx, "修改密码被拒 - 当前角色:%s, 目标:%s, 原因:%v", currentRole, req.Role, err)
		return nil, err
	}

	// 校验的是**当前会话角色**的密码，不是目标账号的：
	// 管理员替普通用户重置密码时，要证明的是管理员本人。
	authDef := passwordDefineFor(normalizeRole(currentRole))
	if authDef == nil {
		return nil, gerror.NewCode(gcode.CodeNotAuthorized, "会话无效，请重新登录")
	}
	if ok, verifyErr := verifyStoredPassword(ctx, authDef, req.OldPassword); verifyErr != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, verifyErr, "密码校验失败")
	} else if !ok {
		c_log.BizWarningf(ctx, "修改密码失败：当前密码不正确 - 当前角色:%s, 目标:%s", currentRole, target)
		return nil, gerror.NewCode(gcode.CodeNotAuthorized, "当前密码不正确")
	}

	targetDef := passwordDefineFor(target)
	if targetDef == nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "角色不支持")
	}

	hashedPassword, err := c_util.HashPassword(req.NewPassword)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "生成密码哈希失败")
	}
	if err := s_db.GetSettingService().SetSettingValueById(ctx, targetDef.Id, hashedPassword); err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "更新密码失败")
	}

	// 注意：已登录的其它会话不会因此失效 —— 会话存在服务端文件里，
	// 改密码不吊销它们。被改密码的账号需要自知或等会话过期。
	c_log.BizInfof(ctx, "用户修改密码 - 操作者角色:%s, 目标账号:%s", currentRole, target)
	return &v1.ChangePasswordRes{}, nil
}

// passwordDefineFor 角色 → 密码设置定义。
func passwordDefineFor(role string) *s_db_basic.SSystemSettingDefine {
	switch role {
	case string(c_enum.EUserRoleAdmin):
		return s_db_basic.SystemSettingAdminPassword
	case string(c_enum.EUserRoleUser):
		return s_db_basic.SystemSettingUserPassword
	default:
		return nil
	}
}

// verifyStoredPassword 比对设置里的密码（兼容旧的明文存储）。
func verifyStoredPassword(ctx context.Context, def *s_db_basic.SSystemSettingDefine, plaintext string) (bool, error) {
	stored := s_db.GetSettingService().GetSettingValueBySystemSettingDefine(ctx, def)
	if stored == nil {
		// 设置缺失 = 该账号根本没有可用密码，登录也进不来；此处失败关闭，不放行
		return false, nil
	}
	return c_util.VerifyPassword(*stored, plaintext)
}
