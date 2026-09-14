package impl

import (
	"common/c_base"
	"context"
	"fmt"
	"s_db/s_db_basic"
	"s_db/s_db_model"
	"sync"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/google/uuid"
)

type sDeviceServiceImpl struct {
	deviceModel *s_db_model.SDeviceModel
	ctx         context.Context
}

var (
	deviceManageInstance s_db_basic.IDeviceService
	deviceManageOnce     sync.Once
)

func GetDeviceService() s_db_basic.IDeviceService {
	deviceManageOnce.Do(func() {
		deviceManageInstance = &sDeviceServiceImpl{
			ctx: context.Background(),
		}
	})
	return deviceManageInstance
}

func (s *sDeviceServiceImpl) UpdateDevice(ctx context.Context, deviceId string, data map[string]interface{}) error {
	device := &s_db_model.SDeviceModel{}
	err := device.GetById(ctx, deviceId)
	if err != nil {
		return err
	}

	if value, ok := data["name"].(string); ok {
		device.Name = value
	}
	if value, ok := data["params"].(string); ok {
		device.Params = value
	}
	if value, ok := data["storageEnable"].(bool); ok {
		device.StorageEnable = value
	}
	if value, ok := data["protocolId"].(string); ok {
		device.ProtocolId = value
	}
	if value, ok := data["driver"].(string); ok {
		device.Driver = value
	}
	if value, ok := data["storageIntervalSec"].(int32); ok {
		device.StorageIntervalSec = value
	}
	if value, ok := data["sort"].(int); ok {
		device.Sort = value
	}
	if value, ok := data["enabled"].(bool); ok {
		device.Enabled = value
	}
	if value, ok := data["enableDebug"].(bool); ok {
		device.EnableDebug = value
	}
	if value, ok := data["manualMode"].(bool); ok {
		device.ManualMode = value
	}
	if value, ok := data["externalParam"].(*c_base.SExternalParam); ok {
		// 获取现有的externalParam数据
		var existingParam c_base.SExternalParam
		if device.ExternalParam != "" && device.ExternalParam != "{}" {
			if err := gjson.DecodeTo(device.ExternalParam, &existingParam); err != nil {
				// 如果解析失败，使用空结构体
				existingParam = c_base.SExternalParam{}
			}
		}

		// 合并新的参数到现有参数中
		if value.ModbusRegisterAddr != 0 {
			existingParam.ModbusRegisterAddr = value.ModbusRegisterAddr
		}
		if value.ModbusId != 0 {
			existingParam.ModbusId = value.ModbusId
		}
		if value.ModbusAllowControl {
			existingParam.ModbusAllowControl = value.ModbusAllowControl
		}

		// 将合并后的结构体转换为JSON字符串存储
		if jsonStr, err := gjson.Encode(existingParam); err == nil {
			device.ExternalParam = string(jsonStr)
		}
	}
	return device.Update(ctx)
}

func (s *sDeviceServiceImpl) GetDeviceConfigsWithRecursion(ctx context.Context, parentId string) ([]*c_base.SDeviceConfig, error) {
	// 查询所有设备（包括enabled=false）
	list, err := g.DB().GetAll(ctx, `
		WITH RECURSIVE DeviceDescendants AS (
			SELECT *
			FROM device
			WHERE id = ?  OR pid = ?
			UNION ALL
			SELECT d.*
			FROM device AS d
					 JOIN DeviceDescendants AS dd ON d.pid = dd.id
		)
		SELECT * FROM DeviceDescendants order by sort
`, g.Slice{parentId, parentId})

	if err != nil {
		return nil, err
	}

	var models []*s_db_model.SDeviceModel
	if convErr := list.Structs(&models); convErr != nil {
		return nil, convErr
	}

	devices := make([]*c_base.SDeviceConfig, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		devices = append(devices, deviceModelToConfig(model))
	}
	return devices, nil
}

// DeleteById 根据ID删除设备记录
func (s *sDeviceServiceImpl) DeleteDeviceById(ctx context.Context, id string) error {
	_, err := g.Model(s.deviceModel).Ctx(ctx).Where(s_db_model.FieldId, id).Delete()
	return err
}

// GetAllDevices 获取所有设备记录
func (s *sDeviceServiceImpl) GetAllDevices(ctx context.Context) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Scan(&devices)
	return devices, err
}

// GetByCondition 根据条件获取设备记录
func (s *sDeviceServiceImpl) GetDevicesByCondition(ctx context.Context, condition g.Map) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Where(condition).Scan(&devices)
	return devices, err
}

func (s *sDeviceServiceImpl) GetRecursiveDevicesByPid(ctx context.Context, pid string) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Where(s_db_model.FieldDevicePid, pid).Scan(&devices)
	if err != nil {
		return nil, err
	}

	for _, device := range devices {
		subDevices, err := s.GetRecursiveDevicesByPid(ctx, device.Id)
		if err != nil {
			return nil, err
		}
		devices = append(devices, subDevices...)
	}

	return devices, nil
}

// GetDevicesById 根据ID获取设备记录
func (s *sDeviceServiceImpl) GetDeviceById(ctx context.Context, id string) (*s_db_model.SDeviceModel, error) {
	var device *s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Where(s_db_model.FieldId, id).Scan(&device)
	return device, err
}

// GetDevicesByPid 根据父设备ID获取子设备列表
func (s *sDeviceServiceImpl) GetDevicesByPid(ctx context.Context, pid string) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Where(s_db_model.FieldDevicePid, pid).Scan(&devices)
	return devices, err
}

// GetByProtocolId 根据协议ID获取设备列表
func (s *sDeviceServiceImpl) GetDevicesByProtocolId(ctx context.Context, protocolId string) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Where(s_db_model.FieldDeviceProtocolId, protocolId).Scan(&devices)
	return devices, err
}

// GetEnabledDevices 获取所有启用的设备
func (s *sDeviceServiceImpl) GetEnabledDevices(ctx context.Context) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Where(s_db_model.FieldEnabled, true).Scan(&devices)
	return devices, err
}

// Count 获取设备总数
func (s *sDeviceServiceImpl) CountDevices(ctx context.Context) (int, error) {
	count, err := g.Model(s.deviceModel).Ctx(ctx).Count()
	return count, err
}

// CountByCondition 根据条件获取设备数量
func (s *sDeviceServiceImpl) CountDevicesByCondition(ctx context.Context, condition g.Map) (int, error) {
	count, err := g.Model(s.deviceModel).Ctx(ctx).Where(condition).Count()
	return count, err
}

// Paginate 分页获取设备列表
func (s *sDeviceServiceImpl) PaginateDevices(ctx context.Context, page, pageSize int) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Page(page, pageSize).Scan(&devices)
	return devices, err
}

// GetAllDevicesOrderBySort 获取所有设备记录，按sort字段排序
func (s *sDeviceServiceImpl) GetAllDevicesOrderBySort(ctx context.Context) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Order(s_db_model.FieldSort).Scan(&devices)
	return devices, err
}

// GetAllDevicesOrderBySortAndEnable 获取所有设备记录，按sort字段排序，enable参数控制是否只获取启用的设备
func (s *sDeviceServiceImpl) GetAllDevicesOrderBySortAndEnable(ctx context.Context, enable bool) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Where(s_db_model.FieldEnabled, enable).Order(s_db_model.FieldSort).Scan(&devices)
	return devices, err
}

// GetDevicesByPidOrderBySort 根据父设备ID获取子设备列表，按sort字段排序
func (s *sDeviceServiceImpl) GetDevicesByPidOrderBySort(ctx context.Context, pid string) ([]*s_db_model.SDeviceModel, error) {
	var devices []*s_db_model.SDeviceModel
	err := g.Model(s.deviceModel).Ctx(ctx).Where(s_db_model.FieldDevicePid, pid).Order(s_db_model.FieldSort).Scan(&devices)
	return devices, err
}

func (s *sDeviceServiceImpl) CreateDevice(ctx context.Context, device *s_db_model.SDeviceModel) (string, error) {
	if device == nil {
		return "", fmt.Errorf("device is nil")
	}
	if device.Id == "" {
		device.Id = uuid.NewString()
	}
	existing, err := s.GetDeviceById(ctx, device.Id)
	if err == nil && existing != nil && existing.Id != "" {
		return "", fmt.Errorf("device id already exists: %s", device.Id)
	}
	now := gtime.Now()
	device.CreatedAt = now
	device.UpdatedAt = now
	if err := device.Create(ctx); err != nil {
		return "", err
	}
	return device.Id, nil
}

func deviceModelToConfig(model *s_db_model.SDeviceModel) *c_base.SDeviceConfig {
	config := &c_base.SDeviceConfig{
		Id:                 model.Id,
		Pid:                model.Pid,
		Name:               model.Name,
		ProtocolId:         model.ProtocolId,
		Driver:             model.Driver,
		EnableDebug:        model.EnableDebug,
		ManualMode:         model.ManualMode,
		StorageEnable:      model.StorageEnable,
		StorageIntervalSec: model.StorageIntervalSec,
		Sort:               model.Sort,
		Enabled:            model.Enabled,
	}
	if model.CreatedAt != nil {
		config.CreatedAt = model.CreatedAt.String()
	}
	if model.UpdatedAt != nil {
		config.UpdatedAt = model.UpdatedAt.String()
	}
	if params := decodeDeviceParams(model.Params); len(params) > 0 {
		config.Params = params
	}
	if external := decodeExternalParam(model.ExternalParam); external != nil {
		config.ExternalParam = external
	}
	return config
}

func decodeDeviceParams(raw string) map[string]any {
	if raw == "" || raw == "{}" || raw == s_db_model.EmptyValue || raw == s_db_model.NullValue {
		return nil
	}
	var params map[string]any
	if err := gjson.DecodeTo(raw, &params); err != nil {
		return nil
	}
	return params
}

func decodeExternalParam(raw string) *c_base.SExternalParam {
	if raw == "" || raw == "{}" || raw == s_db_model.EmptyValue || raw == s_db_model.NullValue {
		return nil
	}
	var external c_base.SExternalParam
	if err := gjson.DecodeTo(raw, &external); err != nil {
		return nil
	}
	return &external
}
