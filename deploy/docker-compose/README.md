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
