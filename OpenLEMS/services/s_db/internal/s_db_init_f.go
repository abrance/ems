package internal

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"common/c_log"
	"common/c_util"
	"s_db/s_db_basic"
	"s_db/s_db_model"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/gctx"
)

var DefaultSqliteDbPath = gcmd.GetOpt("db-path", "./out/db.sqlite3").String()

func Init() error {
	return initConfigDatabase()
}

// 初始化配置数据库
func initConfigDatabase() error {
	ctx := gctx.New()

	// 确保数据库目录存在
	dbPath := DefaultSqliteDbPath
	dbDir := filepath.Dir(dbPath)
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			c_log.Errorf(ctx, "创建数据库目录失败: %+v", err)
			return err
		}
		c_log.Infof(ctx, "创建数据库目录: %s", dbDir)
	}

	// 测试数据库连接（如果文件不存在，SQLite会自动创建）
	if _, err := g.DB().Exec(ctx, "SELECT 1"); err != nil {
		c_log.Errorf(ctx, "数据库连接失败: %+v", err)
		return err
	}

	// 创建协议表
	_, err := g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS protocol (
			id VARCHAR(255) PRIMARY KEY ,
			name VARCHAR(255) NOT NULL,
			type VARCHAR(255) NOT NULL,
			address VARCHAR(255) NOT NULL,
			timeout INTEGER NOT NULL DEFAULT 30,
			log_level VARCHAR(255) NOT NULL DEFAULT 'INFO',
			sort INTEGER DEFAULT 0,
			params TEXT DEFAULT '{}',
		    enabled BOOLEAN DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 创建设备表
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS device (
			id VARCHAR(255) PRIMARY KEY ,
			pid VARCHAR(255) NOT NULL DEFAULT '0',
			protocol_id VARCHAR(255),
			name VARCHAR(255) NOT NULL,
			driver VARCHAR(255),
			enable_debug BOOLEAN DEFAULT 0,
		    manual_mode BOOLEAN DEFAULT FALSE,
			params TEXT DEFAULT '{}',
		    storage_enable BOOLEAN DEFAULT 1,
			storage_interval_sec INTEGER NOT NULL DEFAULT 60,
			external_param TEXT DEFAULT '{}',
			sort INTEGER DEFAULT 0,
		    enabled BOOLEAN DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 创建设置表
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS setting (
			id VARCHAR(255) PRIMARY KEY,
			group_name VARCHAR(255) DEFAULT 'system',
			value TEXT,
		    is_public BOOLEAN DEFAULT 0,
			enabled BOOLEAN default 1,
			remark VARCHAR(255),
			sort INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 创建告警历史表
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS alarm_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id VARCHAR(255) NOT NULL,
			source_device_id VARCHAR(255) NOT NULL,
			point VARCHAR(255) NOT NULL,
			point_name VARCHAR(255) NOT NULL,
			level VARCHAR(255) NOT NULL ,
			detail TEXT,
			trigger_at DATETIME NOT NULL,
			clear_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 告警历史按 trigger_at DESC 翻页、按 device_id 过滤，原先两列均无索引。
	// 30 万行实测：无过滤列表 173.9ms → 6.3ms，按设备列表 52.6ms → 0.6ms，深翻页 58.8ms → 0.8ms。
	// 两条必须成对：只加 (trigger_at DESC) 会让深翻页从 58.8ms 恶化到 113.5ms。
	// 建索引开销随行数线性增长（30 万行合计不到 1s）；表涨到千万行后应把建索引移出启动路径。
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_alarm_history_trigger_at ON alarm_history (trigger_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_alarm_history_device_trigger ON alarm_history (device_id, trigger_at DESC)`,
	} {
		if _, err = g.DB().Exec(ctx, stmt); err != nil {
			c_log.Error(ctx, err)
			return err
		}
	}

	// 创建告警忽略表
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS alarm_ignore (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id VARCHAR(255) NOT NULL,
			source_device_id VARCHAR(255) NOT NULL,
			point VARCHAR(255) NOT NULL,
			point_name VARCHAR(255) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 创建日志表
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type VARCHAR(255) NOT NULL,
			device_id VARCHAR(255),
			level VARCHAR(255) NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 日志列表按 created_at DESC 深翻页、按 device_id 过滤，原先两列均无索引，
	// 30 万行时深翻页实测 252ms，补索引后 4ms。
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_log_created_at ON log (created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_log_device_id ON log (device_id)`,
	} {
		if _, err = g.DB().Exec(ctx, stmt); err != nil {
			c_log.Error(ctx, err)
			return err
		}
	}

	// 创建自动化表
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS automation (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			start_time DATETIME,
			end_time DATETIME,
			time_range_type VARCHAR(255) ,
			time_range_value VARCHAR(255),
			trigger_rule TEXT NOT NULL,
			execute_rule TEXT NOT NULL,
			execution_interval INTEGER DEFAULT 0,
			enabled BOOLEAN DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 创建储能策略表
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS energy_storage (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			priority INTEGER NOT NULL CHECK (priority BETWEEN 1 AND 5),
			status VARCHAR(32) NOT NULL DEFAULT 'inactive',
			is_default BOOLEAN DEFAULT 0,
			date_range TEXT NOT NULL,
			time_range TEXT NOT NULL,
			config TEXT NOT NULL,
			created_by VARCHAR(255) NOT NULL DEFAULT 'admin',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 创建电价表
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS price (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			description VARCHAR(255),
			priority INTEGER NOT NULL,
			status VARCHAR(50) NOT NULL,
			date_range TEXT NOT NULL,
			time_range TEXT NOT NULL,
			price_segments TEXT NOT NULL,
			remote_id VARCHAR(255),
			created_by VARCHAR(255) NOT NULL DEFAULT 'admin',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_spot_report (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_date VARCHAR(10) NOT NULL UNIQUE,
			title VARCHAR(255) NOT NULL,
			source_url TEXT NOT NULL,
			pages TEXT NOT NULL,
			collected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_spot_metric (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date VARCHAR(10) NOT NULL,
			run_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL,
			scope VARCHAR(32) NOT NULL DEFAULT '',
			metric VARCHAR(32) NOT NULL,
			value REAL NOT NULL DEFAULT 0,
			unit VARCHAR(16) NOT NULL DEFAULT '',
			at_time VARCHAR(8) NOT NULL DEFAULT '',
			text_value VARCHAR(32) NOT NULL DEFAULT '',
			mom_pct REAL,
			share_pct REAL,
			source VARCHAR(32) NOT NULL DEFAULT '',
			conf REAL NOT NULL DEFAULT 0,
			extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			origin VARCHAR(8) NOT NULL DEFAULT 'ocr',
			edited_by VARCHAR(64) NOT NULL DEFAULT '',
			edited_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (report_date, run_date, dataset, scope, metric)
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}
	if err := migrateSpotMetricUniqueKey(ctx); err != nil {
		c_log.Error(ctx, err)
		return err
	}
	if err := migrateSpotMetricEditColumns(ctx); err != nil {
		c_log.Error(ctx, err)
		return err
	}

	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_spot_metric_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date VARCHAR(10) NOT NULL,
			run_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL,
			scope VARCHAR(32) NOT NULL DEFAULT '',
			metric VARCHAR(32) NOT NULL,
			old_value REAL,
			new_value REAL,
			old_origin VARCHAR(8) NOT NULL DEFAULT '',
			operation VARCHAR(32) NOT NULL DEFAULT '',
			operator VARCHAR(64) NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_spot_extract_run (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date VARCHAR(10) NOT NULL UNIQUE,
			status VARCHAR(16) NOT NULL DEFAULT '',
			ocr_model VARCHAR(16) NOT NULL DEFAULT '',
			page_count INTEGER NOT NULL DEFAULT 0,
			ocr_ms INTEGER NOT NULL DEFAULT 0,
			warnings TEXT,
			payload TEXT,
			extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 官方 API 采集元数据：哪天采过、成功/失败、源站原始数值。
	// 与 gd_spot_extract_run 分开：那边是 OCR 日报（ocr_model/page_count/ocr_ms），
	// 这边是 pm.gd.csg.cn 接口抓取，混表会让两边的列永远一半是空。
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_pm_fetch_run (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT '',
			rows_written INTEGER NOT NULL DEFAULT 0,
			source_url VARCHAR(255) NOT NULL DEFAULT '',
			payload TEXT,
			warnings TEXT,
			extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (target_date, dataset, extractor_version)
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 电价预测留痕：复盘要回答「当时发的预测准不准」，所以必须落库。
	// 唯一键 (generated_on, market, grain, target_time)：同一天对同一目标时刻只留一条。
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_price_forecast (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			generated_on TEXT NOT NULL,
			market TEXT NOT NULL,
			grain TEXT NOT NULL,
			target_time TEXT NOT NULL,
			point REAL NOT NULL DEFAULT 0,
			q10 REAL NOT NULL DEFAULT 0,
			q50 REAL NOT NULL DEFAULT 0,
			q90 REAL NOT NULL DEFAULT 0,
			model TEXT NOT NULL DEFAULT '',
			unit TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (generated_on, market, grain, target_time)
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}
	_, err = g.DB().Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_price_forecast_target ON gd_price_forecast (market, grain, target_time)`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 官网主页 24 点分时（日前/实时电价 + 系统负荷）。
	// 独立于 gd_spot_metric：粒度（24 整点）与滞后（T-6 左右）都不同，混表会破坏 as-of 语义。
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_home_24h (
			price_date VARCHAR(10) NOT NULL,
			hour INTEGER NOT NULL,
			power_mw REAL NOT NULL DEFAULT 0,
			ahead_price REAL NOT NULL DEFAULT 0,
			realtime_price REAL NOT NULL DEFAULT 0,
			source VARCHAR(64) NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (price_date, hour)
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 21 地市节点电价（高峰/低谷两时段，42 条/日，T+0）。
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_node_price (
			run_date VARCHAR(10) NOT NULL,
			city VARCHAR(32) NOT NULL,
			period_type INTEGER NOT NULL DEFAULT 1,
			price REAL NOT NULL DEFAULT 0,
			source VARCHAR(64) NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (run_date, city, period_type)
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}
	_, err = g.DB().Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_node_price_date ON gd_node_price (run_date)`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	// 日报核对用的「日报原值」表（OCR 链路 D 的落库目标）。
	//
	// 独立于 gd_spot_metric：那张表按「一份日报」成行（唯一键含 report_date），同一运行日
	// 会被表1(二) 与滞后的表2(一) 各写一行；核对页要与官网 1 天 1 行对齐，所以这里唯一键
	// 不含 report_date（report_date 只留痕）。
	//
	// 唯一键为什么含 dataset：核对页要算差值的两行（日前均价 / 实时均价）run_date、scope、
	// metric 完全相同，只有 dataset 不同；不含 dataset 它们会互相覆盖，差值恒为一行。
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_report_daily (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_date VARCHAR(10) NOT NULL,
			report_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL,
			scope VARCHAR(16) NOT NULL,
			metric VARCHAR(16) NOT NULL,
			value REAL NOT NULL,
			unit VARCHAR(16) NOT NULL,
			source VARCHAR(16) NOT NULL,
			origin VARCHAR(8) NOT NULL DEFAULT 'ocr',
			extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			report_title VARCHAR(255) NOT NULL DEFAULT '',
			collected_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (run_date, dataset, scope, metric)
		)
	`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}
	_, err = g.DB().Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_report_daily_run_date ON gd_report_daily (run_date)`)
	if err != nil {
		c_log.Error(ctx, err)
		return err
	}

	c_log.Info(ctx, "Database tables created successfully")

	// 初始化系统设置数据
	initSystemSettings(context.Background())

	return nil
}

// migrateSpotMetricUniqueKey 一次性迁移：gd_spot_metric 初版把唯一键放在
// (run_date, dataset, scope, metric)，但同一运行日的指标会由多份日报产出
// （表2 承载滞后运行日），必须让每份日报各自成行，唯一键因此改为含 report_date。
//
// 该表是新功能产物、数据可由重跑解析恢复，所以直接删表重建；只对 sqlite 生效，
// 其他驱动或表不存在时静默跳过。
func migrateSpotMetricUniqueKey(ctx context.Context) error {
	value, err := g.DB().GetValue(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, "gd_spot_metric")
	if err != nil || value.IsNil() {
		return nil // 非 sqlite 或表刚创建
	}
	if !strings.Contains(value.String(), "UNIQUE (run_date, dataset, scope, metric)") {
		return nil
	}
	c_log.Warning(ctx, "检测到 gd_spot_metric 旧唯一键，删除重建（数据可由重新解析恢复）")
	_, err = g.DB().Exec(ctx, `DROP TABLE IF EXISTS gd_spot_metric`)
	if err != nil {
		return err
	}
	_, err = g.DB().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS gd_spot_metric (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date VARCHAR(10) NOT NULL,
			run_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL,
			scope VARCHAR(32) NOT NULL DEFAULT '',
			metric VARCHAR(32) NOT NULL,
			value REAL NOT NULL DEFAULT 0,
			unit VARCHAR(16) NOT NULL DEFAULT '',
			at_time VARCHAR(8) NOT NULL DEFAULT '',
			text_value VARCHAR(32) NOT NULL DEFAULT '',
			mom_pct REAL,
			share_pct REAL,
			source VARCHAR(32) NOT NULL DEFAULT '',
			conf REAL NOT NULL DEFAULT 0,
			extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (report_date, run_date, dataset, scope, metric)
		)
	`)
	return err
}

// migrateSpotMetricEditColumns 老库补列：origin / edited_by / edited_at（人工修订层新增）。
// sqlite 的 ADD COLUMN 是幂等的轻量操作；列已存在或非 sqlite 时静默跳过。
func migrateSpotMetricEditColumns(ctx context.Context) error {
	columns := []string{"origin", "edited_by", "edited_at", "text_value"}
	for _, name := range columns {
		value, err := g.DB().GetValue(ctx,
			`SELECT COUNT(*) FROM pragma_table_info('gd_spot_metric') WHERE name = ?`, name)
		if err != nil || value.IsNil() {
			return nil // 非 sqlite
		}
		if value.Int() > 0 {
			continue
		}
		ddl := `ALTER TABLE gd_spot_metric ADD COLUMN origin VARCHAR(8) NOT NULL DEFAULT 'ocr'`
		switch name {
		case "edited_by":
			ddl = `ALTER TABLE gd_spot_metric ADD COLUMN edited_by VARCHAR(64) NOT NULL DEFAULT ''`
		case "edited_at":
			ddl = `ALTER TABLE gd_spot_metric ADD COLUMN edited_at DATETIME`
		case "text_value":
			ddl = `ALTER TABLE gd_spot_metric ADD COLUMN text_value VARCHAR(32) NOT NULL DEFAULT ''`
		}
		if _, err := g.DB().Exec(ctx, ddl); err != nil {
			return err
		}
		c_log.Infof(ctx, "gd_spot_metric 补充列 %s", name)
	}
	return nil
}

// initSystemSettings 初始化系统设置数据
func initSystemSettings(ctx context.Context) {
	// 获取所有系统设置定义
	systemSettings := []*s_db_basic.SSystemSettingDefine{
		s_db_basic.SystemSettingActiveDeviceRootId,
		s_db_basic.SystemSettingActivePolicyId,
		s_db_basic.SystemSettingManualControlEnabled,
		s_db_basic.SystemSettingAutomationInternalMilliseconds,
		s_db_basic.SystemSettingDeviceRetentionDays,
		s_db_basic.SystemSettingSystemRetentionDays,
		s_db_basic.SystemSettingLogRetentionDays,
		s_db_basic.SystemSettingSystemEnableDebugLog,
		s_db_basic.SystemSettingLicenseKey,
		s_db_basic.SystemSettingUserPassword,
		s_db_basic.SystemSettingAdminPassword,
		s_db_basic.SystemSettingPasswordLength,
		// 日志级别（全局 + 各业务模块）；默认值必须保持「不过滤」，见定义处的说明
		s_db_basic.SystemSettingLogLevelGlobal,
		s_db_basic.SystemSettingLogLevelEms,
		s_db_basic.SystemSettingLogLevelDevice,
		s_db_basic.SystemSettingLogLevelProtocol,
		s_db_basic.SystemSettingLogLevelPolicy,
		s_db_basic.SystemSettingLogLevelAutomation,
		s_db_basic.SystemSettingLogLevelRemote,
		s_db_basic.SystemSettingMqttConfigList,
		s_db_basic.SystemSettingModbusConfig,
		s_db_basic.SystemSettingSessionAdminTimeout,
		s_db_basic.SystemSettingSessionUserTimeout,
	}

	// 遍历所有系统设置定义，创建默认设置记录
	for index, settingDefine := range systemSettings {
		// 检查设置是否已存在
		existingSetting := &s_db_model.SSettingModel{}
		err := existingSetting.GetById(ctx, settingDefine.Id)
		if err != nil {
			// 设置不存在，创建新的设置记录
			defaultValue := settingDefine.DefaultValue
			if s_db_basic.IsPasswordSettingID(settingDefine.Id) && defaultValue != "" {
				hashedValue, hashErr := c_util.HashPassword(defaultValue)
				if hashErr != nil {
					c_log.Errorf(ctx, "初始化密码设置哈希失败 - 设置ID: %s, 错误: %+v", settingDefine.Id, hashErr)
				} else {
					defaultValue = hashedValue
				}
			}

			newSetting := &s_db_model.SSettingModel{
				SDatabaseBasic: s_db_model.SDatabaseBasic{
					Id: settingDefine.Id,
				},
				Value:    defaultValue,
				IsPublic: settingDefine.IsPublic,
				Enabled:  true,
				Remark:   settingDefine.Remark,
				Sort:     index, // 使用数组中的顺序作为排序
				Group:    settingDefine.Group,
			}

			err = newSetting.Create(ctx)
			if err != nil {
				c_log.Errorf(ctx, "创建系统设置失败 - 设置ID: %s, 错误: %+v", settingDefine.Id, err)
			} else {
				c_log.Infof(ctx, "创建系统设置成功 - 设置ID: %s", settingDefine.Id)
			}
		} else {
			// 设置已存在，检查是否需要更新分组、备注、排序和公开状态信息
			needUpdate := false
			if existingSetting.Group != settingDefine.Group {
				existingSetting.Group = settingDefine.Group
				needUpdate = true
			}
			if existingSetting.Remark != settingDefine.Remark {
				existingSetting.Remark = settingDefine.Remark
				needUpdate = true
			}
			if existingSetting.IsPublic != settingDefine.IsPublic {
				existingSetting.IsPublic = settingDefine.IsPublic
				needUpdate = true
			}
			if existingSetting.Sort != index {
				existingSetting.Sort = index
				needUpdate = true
			}

			if needUpdate {
				err = existingSetting.Update(ctx)
				if err != nil {
					c_log.Errorf(ctx, "更新系统设置失败 - 设置ID: %s, 错误: %+v", settingDefine.Id, err)
				} else {
					c_log.Infof(ctx, "更新系统设置成功 - 设置ID: %s", settingDefine.Id)
				}
			}
		}
	}

	c_log.Info(ctx, "System settings initialized successfully")
}
