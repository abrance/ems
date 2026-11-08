package utils

import (
	v1 "application/api/auth/v1"
	"common/c_enum"
	"crypto/subtle"
	"os"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
)

// EnvAuthBypassRole 临时放开鉴权的环境变量，取值 admin 或 user。
//
// 非空时所有 API 请求跳过登录校验，直接以该角色放行；前端也会注入
// 对应的 localStorage 会话。仅用于内网调试/临时演示，用完必须撤销：
// 该开关会让任何能访问服务的人都拿到同样的角色权限。
const EnvAuthBypassRole = "AUTH_BYPASS_ROLE"

// EnvAPIStaticToken 用于可信自动化请求的静态 API token。
const EnvAPIStaticToken = "EMS_API_STATIC_TOKEN"

// AuthBypassRole 返回归一化后的鉴权绕过角色：admin、user，或空字符串（未开启）。
func AuthBypassRole() string {
	role := strings.ToLower(strings.TrimSpace(os.Getenv(EnvAuthBypassRole)))
	if role == string(c_enum.EUserRoleAdmin) || role == string(c_enum.EUserRoleUser) {
		return role
	}
	return ""
}

// MiddlewareAuth 认证与授权中间件
func MiddlewareAuth(r *ghttp.Request) {
	// 临时放开鉴权：跳过登录校验，按 AUTH_BYPASS_ROLE 指定的角色放行。
	// bypass=admin 时不再做任何角色校验；bypass=user 时保留角色校验，
	// 于是读写分离仍然生效：GET 类接口放行，管理类（POST/PUT/DELETE）依旧 403。
	if bypass := AuthBypassRole(); bypass != "" {
		r.Session.Set("role", bypass)
		r.SetCtxVar("userRole", bypass)
		if bypass != string(c_enum.EUserRoleAdmin) {
			if handler := r.GetServeHandler(); handler != nil {
				required := requiredRole(r, handler.GetMetaTag("role"))
				if !checkRolePermission(bypass, required) {
					r.Response.WriteJson(ghttp.DefaultHandlerResponse{Code: 403, Message: "权限不足"})
					return
				}
			}
		}
		r.Middleware.Next()
		return
	}

	handler := r.GetServeHandler()

	if matchesAPIStaticToken(r) {
		r.SetCtxVar("userRole", string(c_enum.EUserRoleAdmin))
		r.Middleware.Next()
		return
	}

	// 支持通过 Authorization 头传递会话ID（Bearer <token> 或 直接 <token>）
	if auth := r.GetHeader("Authorization"); auth != "" {
		token := auth
		if strings.HasPrefix(strings.ToLower(token), "bearer ") {
			token = strings.TrimSpace(token[7:])
		}
		token = strings.TrimSpace(token)
		if token != "" {
			// 直接设置 SessionId，支持通过 Authorization header 认证
			r.Session.SetId(token)
		}
	}

	// 公共接口跳过鉴权
	if handler != nil && handler.GetMetaTag("noAuth") == "true" {
		r.Middleware.Next()
		return
	}

	// 读取 Session 中的角色
	role := r.Session.MustGet("role").String()
	if role == "" {
		r.Response.WriteJson(ghttp.DefaultHandlerResponse{Code: 401, Message: "未登录或登录已过期"})
		return
	}

	// 检查角色要求：若未标注role，按方法推断(GET=user，其它=admin)
	if handler != nil {
		required := requiredRole(r, handler.GetMetaTag("role"))
		if required != "" && !checkRolePermission(role, required) {
			r.Response.WriteJson(ghttp.DefaultHandlerResponse{Code: 403, Message: "权限不足"})
			return
		}
	}

	// 刷新 Session 过期时间（滑动过期）
	refreshSessionTimeout(r, role)

	// 注入当前用户信息到上下文（例如在控制器可用）
	r.SetCtxVar("userRole", role)

	r.Middleware.Next()
}

func matchesAPIStaticToken(r *ghttp.Request) bool {
	configuredToken := os.Getenv(EnvAPIStaticToken)
	if configuredToken == "" {
		return false
	}

	auth := strings.TrimSpace(r.GetHeader("Authorization"))
	bearerToken := ""
	if len(auth) >= len("Bearer ") && strings.EqualFold(auth[:len("Bearer ")], "Bearer ") {
		bearerToken = strings.TrimSpace(auth[len("Bearer "):])
	}
	apiToken := strings.TrimSpace(r.GetHeader("X-API-Token"))

	bearerMatches := constantTimeTokenMatch(configuredToken, bearerToken)
	headerMatches := constantTimeTokenMatch(configuredToken, apiToken)
	return bearerMatches || headerMatches
}

func constantTimeTokenMatch(expected, actual string) bool {
	if len(expected) != len(actual) || actual == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

// requiredRole 返回接口要求的角色：未显式标注时按方法推断（GET=user，其它=admin）。
func requiredRole(r *ghttp.Request, metaRole string) string {
	if metaRole != "" {
		return metaRole
	}
	if r.Method == "GET" {
		return string(c_enum.EUserRoleUser)
	}
	return string(c_enum.EUserRoleAdmin)
}

// checkRolePermission 角色包含关系：admin ⊇ user
func checkRolePermission(current, required string) bool {
	if current == string(c_enum.EUserRoleAdmin) {
		return true
	}
	return current == required
}

// refreshSessionTimeout 根据角色刷新过期时间
func refreshSessionTimeout(r *ghttp.Request, role string) {
	// 允许认证接口无需刷新
	// 其它请求刷新为自定义时长（单位小时），默认2小时
	// 会话刷新：GoFrame v2 Session 不提供逐会话 TTL API，这里仅作为占位逻辑；
	// 我们把期望的过期秒数写入 Session（expireSeconds），供前端与服务器侧参考。
	// 服务器全局过期时间在 web.go 中通过 SetSessionMaxAge 设置。
	_ = role
}

// 占位引用，避免未使用导入（接口元信息）
var _ = v1.LoginReq{}
