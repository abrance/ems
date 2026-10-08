package s_db_basic

import (
	"common/c_enum"
	"math/rand"
	"time"
)

// generateRandomString 生成指定长度的随机字符串
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[r.Intn(len(charset))]
	}
	return string(b)
}

// IsPasswordSettingID reports whether the setting stores a login password.
func IsPasswordSettingID(id string) bool {
	return id == SystemSettingUserPassword.Id || id == SystemSettingAdminPassword.Id
}

// 系统设置定义变量
var (
	// 根设备ID设置定义
	SystemSettingActiveDeviceRootId = &SSystemSettingDefine{
		Id:           "active_device_root_id",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "0",
		IsPublic:     true,
		Remark:       "根设备ID",
		FieldType:    c_enum.ESettingFieldTypeText,
	}

	// 激活的策略ID设置定义
	SystemSettingActivePolicyId = &SSystemSettingDefine{
		Id:           "active_policy_id",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "",
		IsPublic:     true,
		Remark:       "激活的策略ID",
		FieldType:    c_enum.ESettingFieldTypeText,
	}

	// 自动化任务轮询周期设置定义
	SystemSettingAutomationInternalMilliseconds = &SSystemSettingDefine{
		Id:           "automation_internal_milliseconds",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "1000",
		IsPublic:     true,
		Remark:       "自动化任务轮询周期（毫秒）",
		FieldType:    c_enum.ESettingFieldTypeNumber,
	}

	// 手动控制（/control/device）总开关，**默认关闭**。
	//
	// 为什么默认关：手动指令与运行中的策略会互相覆盖 —— 策略每个周期都会按自己的决策
	// 重写目标功率，手动下发 12 kW 后大约一分钟就被压回去（线上实测），而当时没有任何
	// 「临时接管」机制，用户看不到这件事。与其留一个会静默被覆盖的按钮，不如先关掉，
	// 等接管机制（手动作业 N 分钟内策略不插手）做好再默认打开。
	// 打开方式：把该设置改成 true（无需改代码）。
	SystemSettingManualControlEnabled = &SSystemSettingDefine{
		Id:           "manual_control_enabled",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "false",
		IsPublic:     true,
		Remark:       "允许手动控制（手动指令会被运行中的策略覆盖，默认关闭）",
		FieldType:    c_enum.ESettingFieldTypeText,
	}

	// 设备数据保留天数设置定义
	SystemSettingDeviceRetentionDays = &SSystemSettingDefine{
		Id:           "DeviceRetentionDays",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "100",
		IsPublic:     true,
		Remark:       "设备数据保留天数",
		FieldType:    c_enum.ESettingFieldTypeNumber,
	}

	// 系统数据保留天数设置定义
	SystemSettingSystemRetentionDays = &SSystemSettingDefine{
		Id:           "SystemRetentionDays",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "7",
		IsPublic:     true,
		Remark:       "系统数据保留天数",
		FieldType:    c_enum.ESettingFieldTypeNumber,
	}

	// 全局日志级别设置定义
	//
	// 管的是**系统日志**（c_log.Info / Infof 这类，落到文件与控制台），
	// 也就是 GoFrame logger 的级别。默认 ALL 与配置里的 logger.level 一致。
	SystemSettingLogLevelGlobal = &SSystemSettingDefine{
		Id:           "LogLevelGlobal",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "ALL",
		IsPublic:     true,
		Remark:       "全局日志级别（ALL / DEBUG / INFO / WARN / ERROR）",
		FieldType:    c_enum.ESettingFieldTypeText,
	}

	// 业务日志各级别设置定义。
	//
	// 管的是**业务日志**（c_log.BizInfo / BizErrorf 这类，按模块入库 + biz 输出）。
	// 默认 DEBUG = 什么都不滤，与加这个功能之前的行为完全一致 ——
	// 默认值不能悄悄改变现网日志行为。
	SystemSettingLogLevelEms = &SSystemSettingDefine{
		Id:           "LogLevelEms",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "DEBUG",
		IsPublic:     true,
		Remark:       "ems 业务日志级别",
		FieldType:    c_enum.ESettingFieldTypeText,
	}
	SystemSettingLogLevelDevice = &SSystemSettingDefine{
		Id:           "LogLevelDevice",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "DEBUG",
		IsPublic:     true,
		Remark:       "device 业务日志级别",
		FieldType:    c_enum.ESettingFieldTypeText,
	}
	SystemSettingLogLevelProtocol = &SSystemSettingDefine{
		Id:           "LogLevelProtocol",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "DEBUG",
		IsPublic:     true,
		Remark:       "protocol 业务日志级别",
		FieldType:    c_enum.ESettingFieldTypeText,
	}
	SystemSettingLogLevelPolicy = &SSystemSettingDefine{
		Id:           "LogLevelPolicy",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "DEBUG",
		IsPublic:     true,
		Remark:       "policy 业务日志级别",
		FieldType:    c_enum.ESettingFieldTypeText,
	}
	SystemSettingLogLevelAutomation = &SSystemSettingDefine{
		Id:           "LogLevelAutomation",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "DEBUG",
		IsPublic:     true,
		Remark:       "automation 业务日志级别",
		FieldType:    c_enum.ESettingFieldTypeText,
	}
	SystemSettingLogLevelRemote = &SSystemSettingDefine{
		Id:           "LogLevelRemote",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "DEBUG",
		IsPublic:     true,
		Remark:       "remote 业务日志级别",
		FieldType:    c_enum.ESettingFieldTypeText,
	}

	// 日志数据保留天数设置定义
	SystemSettingLogRetentionDays = &SSystemSettingDefine{
		Id:           "LogRetentionDays",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "30",
		IsPublic:     true,
		Remark:       "日志数据保留天数",
		FieldType:    c_enum.ESettingFieldTypeNumber,
	}

	// 系统调试日志开关设置定义
	SystemSettingSystemEnableDebugLog = &SSystemSettingDefine{
		Id:           "SystemEnableDebugLog",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "false",
		IsPublic:     true,
		Remark:       "启用系统调试日志",
		FieldType:    c_enum.ESettingFieldTypeBoolean,
	}

	// 软件许可证号设置定义
	SystemSettingLicenseKey = &SSystemSettingDefine{
		Id:           "license_key",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "",
		IsPublic:     false,
		Remark:       "软件许可证号",
		FieldType:    c_enum.ESettingFieldTypeText,
	}

	// 普通用户密码设置定义
	SystemSettingUserPassword = &SSystemSettingDefine{
		Id:           "user_password",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "111111",
		IsPublic:     false,
		Remark:       "普通用户密码",
		FieldType:    c_enum.ESettingFieldTypeText,
	}

	// 管理员用户密码设置定义
	//
	// 初始密码为 admin（与登录页提示一致）。**出厂默认密码等于弱口令**，
	// 部署后第一件事就是改掉它 —— 尤其别把管理端放到公网。
	// 注意：改这里只影响**新建**的库；已有库里 admin_password 已存在，不会被覆盖
	// （刻意如此：升级时重置用户改过的密码比弱默认值更危险）。
	SystemSettingAdminPassword = &SSystemSettingDefine{
		Id:           "admin_password",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "admin",
		IsPublic:     false,
		Remark:       "管理员用户密码",
		FieldType:    c_enum.ESettingFieldTypeText,
	}

	// 密码长度设置定义
	SystemSettingPasswordLength = &SSystemSettingDefine{
		Id:           "password_length",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "6",
		IsPublic:     true,
		Remark:       "密码长度",
		FieldType:    c_enum.ESettingFieldTypeNumber,
	}

	// MQTT配置设置定义
	SystemSettingMqttConfigList = &SSystemSettingDefine{
		Id:           "mqtt_config_list",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "[]",
		IsPublic:     false,
		Remark:       "MQTT配置列表",
		FieldType:    c_enum.ESettingFieldTypeJsonArray,
	}

	// Modbus配置设置定义
	SystemSettingModbusConfig = &SSystemSettingDefine{
		Id:           "modbus_config",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "{}",
		IsPublic:     false,
		Remark:       "Modbus配置",
		FieldType:    c_enum.ESettingFieldTypeJson,
	}

	// 管理员会话超时时间（小时）
	SystemSettingSessionAdminTimeout = &SSystemSettingDefine{
		Id:           "session_admin_timeout",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "2",
		IsPublic:     false,
		Remark:       "管理员Session过期时间（小时）",
		FieldType:    c_enum.ESettingFieldTypeNumber,
	}

	// 普通用户会话超时时间（小时）
	SystemSettingSessionUserTimeout = &SSystemSettingDefine{
		Id:           "session_user_timeout",
		Group:        c_enum.ESettingGroupSystem,
		DefaultValue: "2",
		IsPublic:     false,
		Remark:       "普通用户Session过期时间（小时）",
		FieldType:    c_enum.ESettingFieldTypeNumber,
	}
)

// SLogLevelSetting 一个业务日志模块与它的级别设置定义
type SLogLevelSetting struct {
	Type   c_enum.ESystemGroupType
	Def    *SSystemSettingDefine
	Remark string
}

// LogLevelSettings 业务日志模块 → 级别设置，**顺序即界面顺序**。
// 加一个日志模块只改这里与它的 Define —— 界面的矩阵是按这份列表渲染的。
var LogLevelSettings = []SLogLevelSetting{
	{Type: c_enum.ELogTypeEms, Def: SystemSettingLogLevelEms, Remark: "系统自身（启动、配置、Web、鉴权等）"},
	{Type: c_enum.ELogTypeDevice, Def: SystemSettingLogLevelDevice, Remark: "设备采集与设备控制"},
	{Type: c_enum.ELogTypeProtocol, Def: SystemSettingLogLevelProtocol, Remark: "协议（Modbus / CAN / GPIO 等）"},
	{Type: c_enum.ELogTypePolicy, Def: SystemSettingLogLevelPolicy, Remark: "策略（充放电决策等）"},
	{Type: c_enum.ELogTypeAutomation, Def: SystemSettingLogLevelAutomation, Remark: "自动化联动"},
	{Type: c_enum.ELogTypeRemote, Def: SystemSettingLogLevelRemote, Remark: "远程调度与远程协议"},
}

// LogLevelSettingFor 取某个日志模块的级别设置定义；没有配置项时返回 nil（调用方应按「不过滤」处理）。
func LogLevelSettingFor(t c_enum.ESystemGroupType) *SSystemSettingDefine {
	for _, it := range LogLevelSettings {
		if it.Type == t {
			return it.Def
		}
	}
	return nil
}
