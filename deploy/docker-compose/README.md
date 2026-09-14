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

同 compose 会启动 `ess_demo` Modbus 从站，监听 `1502`。OpenLEMS 协议地址填 `emsdevice`，端口 `1502`，设备驱动 `ess_demo_v1.0.0`，`params.unitId=1`。

本机调试：

```bash
cd emsdevice
go run ./cmd/emsdevice --listen tcp://127.0.0.1:1502 --unit-id 1
```
