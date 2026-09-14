# emsdevice

OpenLEMS `ess_demo` Modbus TCP slave. OpenLEMS is the master and connects to this process.

```bash
cd emsdevice
go test ./...
go build -o emsdevice ./cmd/emsdevice
./emsdevice --listen tcp://0.0.0.0:1502 --unit-id 1
```

Serves holding registers `0xC8-0xD5` (FC03/FC06), matching `ess_demo_v1.0.0`.

## Wire to OpenLEMS

1. Protocol: type `modbus_tcp`, address `emsdevice` (compose) or `127.0.0.1`, port `1502`.
2. Device: `driver=ess_demo_v1.0.0`, `params={"unitId":1}`, `enabled=1`, parent `pid=0`.
3. Setting `active_device_root_id` to that device id, then restart OpenLEMS.
