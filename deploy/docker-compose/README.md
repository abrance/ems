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

## 备份与恢复

数据全在一个 SQLite 文件里：容器内 `/app/out/db.sqlite3`，落在命名卷 `lems-data`
（生产实际卷名 `lems_lems-data`）。v1.0.65 起库是 **WAL 模式**，同目录还有
`db.sqlite3-wal` 与 `db.sqlite3-shm`。

> **不能 `cp db.sqlite3`。** WAL 里未 checkpoint 的写入不在主文件里，拷出来是残缺快照，
> 而且恢复时不报错。必须用 SQLite 原生 Backup API（`sqlite3 .backup`）。

### 备份

```bash
# 有 cron 的机器（需要 docker 权限即可，不需要 root）
0 3 * * * BACKUP_DIR=/opt/cops/backup/lems /path/to/backup.sh >> /var/log/lems-backup.log 2>&1

# 手工跑一次
./deploy/docker-compose/backup.sh
```

**cloud2 现状**：该机没有 crond、没有 root、user linger 也开不了，宿主机上**没有可用的
cron**。所以调度落在常驻容器 `ems-backup`（`ems-backup:2`）里的 crond 上，容器内
`0 3 * * *` 直接执行同一份 `backup.sh`。容器只读挂生产卷、挂备份目录与异地密钥，
不写生产库。改脚本后 `cd /opt/cops/backup/lems-tools-v2 && docker compose up -d --force-recreate`。

流程：`sqlite3 .backup` 出一致快照（不停容器）→ 校验 `PRAGMA integrity_check`
并统计关键表行数 → gzip → scp 到异地并核对字节数 → 按保留策略清本地。

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `BACKUP_DIR` | `/opt/cops/backup/lems` | 本地目录 |
| `OFFSITE` | `nas:/pool1_8t/public/backup/lems` | `ssh:path`；`OFFSITE=` 关闭异地 |
| `KEEP_OFFSITE` | `7` | 异地保留份数 |
| `SSH_KEY` / `SSH_KNOWN_HOSTS` | 空 | 容器内跑时显式指定（容器里没有 `~/.ssh/config`）|
| `KEEP_DAILY` | `7` | 本地保留日备份数 |
| `KEEP_WEEKLY` | `4` | 本地保留周备份数（UTC 周日那份） |
| `VOLUME` | `lems_lems-data` | 数据卷 |

保留策略：最新 7 份全留，另外 UTC 周日那份额外留 4 份，**异地只留最新 1 份日备**。
要改只改脚本顶部这一段。

失败行为：任何一步失败都**非零退出**并写 `FAILED:` 到 stderr，cron 日志里直接可见；
`FAILED-*` 的半成品会留在目录里而不是被当成可用备份。**异地传失败不会删本地**——
先传成功并核对大小，才进入清理阶段。同一时刻有 `flock` 互斥，两份不会互相覆盖。

### 恢复

```bash
./deploy/docker-compose/restore.sh /opt/cops/backup/lems/db-20261112T121406Z.sqlite3.gz
```

流程：解压 + `integrity_check`（不过就**什么都不动**）→ 停 `lems` → 现存库改名
`db.sqlite3.pre-restore-<UTC>`（**保留，不删**）→ 删 `-wal`/`-shm` → 放回备份 →
容器停止状态下核行数 → 起容器 → 等 `/api/health` 返回 200。

`-wal`/`-shm` 必须删：它们属于旧库，残留会被当成新库的未提交事务回放，数据错乱。

### 恢复后可能看到什么

- **告警历史不会变多。** v1.0.66 起 `alarm_history` 全表封顶 1000 条（PR #129）。
  备份的价值是防灾难——设备、点位、协议、配置、日志——**不是**找回已滚出窗口的告警。
  别把备份当成 1000 条上限的解药。
- **升级到 v1.0.66 后的第一条新告警**会触发一次性大清理（30 万 → 1000，走主键范围扫，
  秒级，不长锁表）。从旧备份恢复到 v1.0.66 时看到这一步是正常的。
- **生产钉的是具体 tag，不是 `latest`。** `latest` 会动，用 `docker compose pull` 时
  确认 `.env` 里是 `LEMS_IMAGE_TAG=v1.0.x`。

### 演练

`docker-compose.drill.yml` 是独立项目（`lems-drill`）+ 独立卷（`lems-drill_lems-data`）
+ 独立端口（18080），与生产零交集：

```bash
cd deploy/docker-compose
cp .env.example .env.drill        # 改 LEMS_IMAGE_TAG 到目标版本
docker compose -p lems-drill -f docker-compose.drill.yml --env-file .env.drill up -d
PROJECT=lems-drill CONTAINER=lems-drill VOLUME=lems-drill_lems-data \
  HEALTH_URL=http://127.0.0.1:18080/api/health ./restore.sh <备份>
docker compose -p lems-drill -f docker-compose.drill.yml --env-file .env.drill down
docker volume rm lems-drill_lems-data
```

实测（cloud2，2026-11-12，库 13.7 MB / 压缩后 1.0 MB）：
恢复 4 秒，v1.0.65→v1.0.66 升级 55 秒，v1.0.66 库回滚到 v1.0.65 32 秒，三次 health 均 200、
行数一致。按每天 03:00 备份算，**RPO ≤ 24h，RTO ≈ 1 分钟**（恢复 + 启动 + health 通过）。

### 生产落点（cloud2，2026-11-12 实测）

| 项 | 值 |
| --- | --- |
| 主机 | cloud2（10.193.0.9） |
| compose 项目 | `lems`，文件 `/opt/cops/apps/lems/compose.yaml`（**不是**本目录，本目录是模板） |
| 镜像 | `ghcr.chenby.cn/abrance/ems:v1.0.65`，digest `sha256:ff45a34d...` |
| 重启策略 | `unless-stopped` |
| 卷 | `lems_lems-data` → `/var/lib/docker/volumes/lems_lems-data/_data` |
| 库 | `db.sqlite3` 13.7 MB（WAL，`-wal` 常态 0 B） |
| 健康检查 | `GET http://127.0.0.1:11000/api/health` |
| 异地 | cloud2→nas 免密 SSH，`/pool1_8t/public/backup/lems` |
