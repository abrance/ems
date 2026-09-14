# emsdevice

OpenLEMS `ess_demo` Modbus TCP slave. OpenLEMS is the master and connects to this process.

```bash
cd emsdevice
go test ./...
go build -o emsdevice ./cmd/emsdevice
./emsdevice --listen tcp://0.0.0.0:1502 --unit-id 1
```

Serves holding registers `0xC8-0xD5` (FC03/FC06), matching `ess_demo_v1.0.0`.

## Mock data

Default mode is `static`: registers come from config and stay put except for master writes (FC06).

`live` ticks SOC/power around `targetPower`, respects `deviceControl`, and clamps to min/max SOC.

```bash
./emsdevice --mock live --mock-soc 80 --mock-power 5
./emsdevice --config config.example.json
```

Load order: defaults, JSON `--config` / `EMSDEVICE_CONFIG`, env `EMSDEVICE_*`, then flags.

| Parameter | Env | Meaning |
| --- | --- | --- |
| `--mock` | `EMSDEVICE_MOCK` | `static` / `live` (`true` means live) |
| `--mock-interval` | `EMSDEVICE_MOCK_INTERVAL` | live tick, default `1s` |
| `--mock-soc` | `EMSDEVICE_MOCK_SOC` | SOC percent |
| `--mock-power` | `EMSDEVICE_MOCK_POWER` | power kW |

## Wire to OpenLEMS

1. Protocol: type `modbus_tcp`, address `emsdevice` (compose) or `127.0.0.1`, port `1502`.
2. Device: `driver=ess_demo_v1.0.0`, `params={"unitId":1}`, `enabled=1`, parent `pid=0`.
3. Setting `active_device_root_id` to that device id, then restart OpenLEMS.
