# emsdevice

OpenLEMS 模拟从站：一个 Modbus TCP server，按点表提供光伏 / 负载 / 关口电表 /
储能柜四类设备的 holding registers，OpenLEMS（主站）连上来读、写。

```bash
cd emsdevice
go test ./...
go build -o emsdevice ./cmd/emsdevice
./emsdevice --listen tcp://0.0.0.0:1502 --unit-id 1
```

## 点表（config 里的 `devices`）

`emsdevice.json` 的 `devices` 数组就是点表，每个设备一组点位：

```json
{
  "devices": [
    {
      "key": "pv",
      "name": "光伏",
      "points": [
        { "addr": "0x65", "key": "Power", "name": "当前功率", "type": "int16",
          "scale": 0.1, "value": 60, "jitter": 3, "min": 0, "max": 120 },
        { "addr": "0x68", "key": "PowerLimit", "type": "int16", "scale": 0.1,
          "value": 100, "writable": true }
      ]
    }
  ]
}
```

| 字段 | 说明 |
| --- | --- |
| `addr` | 寄存器地址，支持十进制 `104` 或十六进制字符串 `"0x68"` |
| `key` | 点位标识，与 OpenLEMS 驱动里的点位同名（便于对照） |
| `type` | `int16`（默认）/ `uint16`，决定解码符号与取值上下限 |
| `scale` | 工程量 = 原码 × scale，默认 1 |
| `value` | 上电初值（工程量） |
| `writable` | 允许 FC06/FC16 写入；**不写就是只读** |
| `jitter` | live 模式下每 tick 围绕初值抖动的幅度（工程量），0/缺省 = 不动 |
| `min` / `max` | 抖动后的钳位范围（可选，各出现一个即生效） |

内置的四张表与 OpenLEMS 的 demo 驱动一一对应（地址不重叠，可挂在同一
`unitId` 上）：

| key | 驱动 | holding | 点位数 |
| --- | --- | --- | --- |
| `ess` | `ess_demo_v1.0.0` | `0xC8-0xD5` | 14 |
| `pv` | `pv_demo_v1.0.0` | `0x64-0x6C` | 9 |
| `load` | `load_demo_v1.0.0` | `0x12C-0x131` | 6 |
| `ammeter` | `ammeter_demo_v1.0.0` | `0x190-0x1A8` | 25 |

可写点位（其余一律只读，写读点位返回 `IllegalDataAddress`）：
储能柜 `MaxChargePower` / `MaxDischargePower` / `DeviceControl` / `TargetPower` /
`MinSOC` / `MaxSOC` / `ChargeEfficiency`，光伏 `PowerLimit`。

配置里没有 `devices` 时退回旧行为：只提供内置 `ess_demo` 表，且任意在范围内的
寄存器都可写（向后兼容旧的 `emsdevice.json`）。

## Mock 数据

默认 `static`：寄存器按点表初值保持不变，只有主站写入会改它。

`live` 模式做两件事：

1. 储能柜按 SOC/功率模型运行：绕 `targetPower` 抖动、受 `deviceControl` 控制、
   按 `minSoc`/`maxSoc` 钳位，并把充放电量累计进 `generatedEnergy`/`consumedEnergy`。
2. 点表里带 `jitter` 的点（光伏/负载/关口）每 tick 围绕初值抖动并钳到
   `min`/`max`。刻意不做累积式随机游走 —— 那样会一路漂走，几小时后贴死在
   边界上（实测 20 分钟里辐照度 450→32、关口功率 25→44）。

储能柜的初值仍由 `mock` 段与 `EMSDEVICE_MOCK_*` 环境变量决定（后者优先），
点表里的 `value` 只是同一份默认值的文档副本。

```bash
./emsdevice --mock live --mock-soc 80 --mock-power 5
./emsdevice --config config.example.json
```

加载顺序：内置默认值 → JSON `--config` / `EMSDEVICE_CONFIG` → 环境变量
`EMSDEVICE_*` → 命令行参数。

| 参数 | 环境变量 | 含义 |
| --- | --- | --- |
| `--mock` | `EMSDEVICE_MOCK` | `static` / `live`（`true` 视为 live） |
| `--mock-interval` | `EMSDEVICE_MOCK_INTERVAL` | live tick 间隔，默认 `1s` |
| `--mock-soc` | `EMSDEVICE_MOCK_SOC` | SOC 初值（%） |
| `--mock-power` | `EMSDEVICE_MOCK_POWER` | 功率初值（kW） |

## 接到 OpenLEMS

四类设备共用同一个从站（同一地址/端口/unitId），靠寄存器地址区分：

1. 协议：type `modbus_tcp`，address `emsdevice`（compose）或 `127.0.0.1`，
   端口 `1502`。
2. 设备：`driver=ess_demo_v1.0.0` / `pv_demo_v1.0.0` / `load_demo_v1.0.0` /
   `ammeter_demo_v1.0.0`，`params={"unitId":1}`，`enabled=1`，parent `pid=0`。
3. 把 `active_device_root_id` 指向储能柜设备 id，再重启 OpenLEMS。
