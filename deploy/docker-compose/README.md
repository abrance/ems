# LEMS Docker Compose

使用 CI 推送到 GHCR 的镜像，在服务器上启动 LEMS。

## 部署

```bash
cd deploy/docker-compose
cp .env.example .env
```

按需修改 `.env` 里的 `LEMS_IMAGE_TAG`（例如 `v1.0.0`）。

GHCR 包如果是私有的，先登录：

```bash
echo "$GITHUB_TOKEN" | docker login ghcr.io -u USERNAME --password-stdin
```

启动：

```bash
docker compose up -d
```

默认映射 `http://服务器IP:80`。换版本时改 `LEMS_IMAGE_TAG` 后再次执行 `docker compose up -d`。

## emsdevice

同 compose 会启动模拟 Modbus 从站（监听 `1502`），挂载 `emsdevice.json`。
OpenLEMS 协议地址填 `emsdevice`，端口 `1502`，`params.unitId=1`，按寄存器地址挂四类设备：

| 设备驱动 | holding |
| --- | --- |
| `ess_demo_v1.0.0` 储能柜 | `0xC8-0xD5` |
| `pv_demo_v1.0.0` 光伏 | `0x64-0x6C` |
| `load_demo_v1.0.0` 负载 | `0x12C-0x131` |
| `ammeter_demo_v1.0.0` 关口电表 | `0x190-0x1A8` |

`emsdevice.json` 的 `devices` 段就是点表（地址 / 类型 / 缩放 / 初值 / 是否可写 /
live 抖动）。改完 `docker compose up -d emsdevice` 生效；`.env` 里的
`EMSDEVICE_MOCK*` 覆盖储能柜的同名初值，`EMSDEVICE_MOCK=static` 关闭实时抖动。
四类设备共用一个从站，不再需要额外的端口。
