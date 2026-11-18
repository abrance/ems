# LEMS Docker Compose

使用 CI 推送到 GHCR 的镜像，在服务器上启动 LEMS。

## 部署

```bash
cd deploy/docker-compose
cp .env.example .env
```

按需修改 `.env` 里的 `LEMS_IMAGE_TAG`（例如 `v1.0.0`）。

仓库里的 `config.yaml` **不内置任何外部地址**；数据源/对象存储地址由 `.env` 注入：

| 变量 | 用途 | 留空时 |
| --- | --- | --- |
| `EMS_DB_LINK` | 关系库连接串（sqlite 或 pgsql）。支持 `postgresql://user:pass@host:5432/db` 与 gf 的 `pgsql:user:pass@tcp(host:5432)/db` 两种写法 | 用 `config.yaml` 里的本地 sqlite |
| `PRICE_SYNC_BASE_URL` | 电网分时电价数据源 | 同步任务跳过 |
| `HOME_MARKET_GATEWAY_BASE` | 官网主页 24 点/地市网关 | 采集不启动 |
| `GD_METRIC_URL` | 第三方 `gd_metric.csv` 下载地址 | 不联网下载 |
| `SPOT_REPORT_WECHAT_HOST` | 允许的公众号文章域名 | 用公开平台域名兜底 |
| `QINIU_*` | 日报图片对象存储 | 存本地 |

`FORECAST_BASE_URL` 只影响插件默认值，实际以策略配置 `forecastBaseUrl` 为准。

**切 PostgreSQL**：设 `EMS_DB_LINK` 后重启即可，建表语句会按方言自动转换（`AUTOINCREMENT`→`BIGSERIAL`、
`DATETIME`→`TIMESTAMP`、`BOOLEAN DEFAULT 1/0`→`TRUE/FALSE`）。时区固定 `Asia/Shanghai`——PG 不接受 `Local`。
存量数据从本地 sqlite 搬过去：

```bash
cd OpenLEMS/services/s_db
EMS_DB_LINK=postgresql://user:pass@host:5432/db \
  go run ./cmd/sqlite2pg -src ../../application/out/db.sqlite3 -truncate
```

先让服务带着 `EMS_DB_LINK` 起一次（建表），再跑工具；`-dry-run` 先看行数对比。

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
0 3 * * * BACKUP_DIR=/var/backups/lems /path/to/backup.sh >> /var/log/lems-backup.log 2>&1

# 手工跑一次
./deploy/docker-compose/backup.sh
```

**生产机现状**：该机没有 crond、没有 root、user linger 也开不了，宿主机上**没有可用的
cron**。所以调度落在常驻容器 `ems-backup`（`ems-backup:2`）里的 crond 上，容器内
`0 3 * * *` 直接执行同一份 `backup.sh`。容器只读挂生产卷、挂备份目录与异地密钥，
不写生产库。改脚本后需要重建备份容器（`docker compose up -d --force-recreate`）。

流程：`sqlite3 .backup` 出一致快照（不停容器）→ 校验 `PRAGMA integrity_check`
并统计关键表行数 → gzip → scp 到异地并核对字节数 → 按保留策略清本地。

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `BACKUP_DIR` | `/var/backups/lems` | 本地目录 |
| `OFFSITE` | 空 | `ssh:path`（如 `backup-host:/path`）；默认不做异地 |
| `KEEP_OFFSITE` | `7` | 异地保留份数 |
| `SSH_KEY` / `SSH_KNOWN_HOSTS` | 空 | 容器内跑时显式指定（容器里没有 `~/.ssh/config`）|
| `KEEP_DAILY` | `7` | 本地保留日备份数 |
| `KEEP_WEEKLY` | `4` | 本地保留周备份数（UTC 周日那份） |
| `VOLUME` | `lems_lems-data` | 数据卷 |

保留策略：本地最新 7 份全留，另外 UTC 周日那份额外留 4 份；**异地按 `KEEP_OFFSITE`（默认 7）留最新 7 份**。
要改只改脚本顶部这一段。超过 24h 的 `*.part` 半成品与 `FAILED-*` 标记也会被顺手清掉
（prune 只认 `db-*.sqlite3.gz`，这两类不在它的匹配范围内）。

失败行为：任何一步失败都**非零退出**并写 `FAILED:` 到 stderr，cron 日志里直接可见；
`FAILED-*` 的半成品会留在目录里而不是被当成可用备份。**异地传失败不会删本地**——
先传成功并核对大小，才进入清理阶段。同一时刻有 `flock` 互斥，两份不会互相覆盖。

### 恢复

```bash
# 生产：必须显式 --yes，否则只打印「将停止 lems、把 lems_lems-data/db.sqlite3 改名留底」并退出 1
./deploy/docker-compose/restore.sh /var/backups/lems/db-<UTC>.sqlite3.gz --yes
# 也可以指定 health 地址（默认按 LEMS_PORT 从生产 .env 取，兜底 11000）
HEALTH_URL=http://127.0.0.1:11000/api/health \
  ./deploy/docker-compose/restore.sh /var/backups/lems/db-<UTC>.sqlite3.gz --yes
```

**生产护栏**：`CONTAINER=lems` 或 `VOLUME=lems_lems-data`（两个默认值）时必须带 `--yes`
或 `PROD_CONFIRM=1`，否则脚本只打印将要动的容器与卷名后 `exit 1`。演练用显式变量绕开。

`HEALTH_URL` 默认是 `http://127.0.0.1:<LEMS_PORT>/api/health`，`LEMS_PORT` 从
生产 `.env`（`LEMS_ENV`）读、兜底 `11000`。**生产宿主映射是 `80/tcp -> 0.0.0.0:11000`**，
所以不能默认写 80——那会让恢复成功后仍判 health 失败退出 1。

流程：解压 + `integrity_check` + 取备份行数（任一步不过就**什么都不动**，容器不会被停）→
停 `lems` → 现存库改名
`db.sqlite3.pre-restore-<UTC>`（**保留，不删**）→ 删 `-wal`/`-shm` → 放回备份 →
容器停止状态下核行数 → 起容器 → 等 `/api/health` 返回 200。

`-wal`/`-shm` 必须删：它们属于旧库，残留会被当成新库的未提交事务回放，数据错乱。

失败路径：停容器之后的任何一步失败都会打印「容器仍处于停止状态，执行 `docker start <容器>` 恢复」，
现存库已改名留底、没有删除，可原样换回。

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
cp .env.drill.example .env.drill  # 改 LEMS_IMAGE_TAG 到目标版本
docker compose -p lems-drill -f docker-compose.drill.yml --env-file .env.drill up -d
PROJECT=lems-drill CONTAINER=lems-drill VOLUME=lems-drill_lems-data \
  HEALTH_URL=http://127.0.0.1:18080/api/health ./restore.sh <备份>
docker compose -p lems-drill -f docker-compose.drill.yml --env-file .env.drill down
docker volume rm lems-drill_lems-data
```

实测（生产机，2026-11-12，库 13.7 MB / 压缩后 1.0 MB）：
恢复 4 秒，v1.0.65→v1.0.66 升级 55 秒，v1.0.66 库回滚到 v1.0.65 32 秒，三次 health 均 200、
行数一致。按每天 03:00 备份算，**RPO ≤ 24h，RTO ≈ 1 分钟**（恢复 + 启动 + health 通过）。

### 生产落点（模板，实际值以部署机的 `.env` 为准）

| 项 | 值 |
| --- | --- |
| 主机 | `<prod-host>` |
| compose 项目 | `lems`，文件 `<deploy-root>/compose.yaml`（**不是**本目录，本目录是模板） |
| 镜像 | `${LEMS_IMAGE}:${LEMS_IMAGE_TAG}` |
| 重启策略 | `unless-stopped` |
| 卷 | `lems_lems-data` |
| 库 | `db.sqlite3`（WAL） |
| 健康检查 | `GET http://127.0.0.1:<LEMS_PORT>/api/health` |
| 异地 | `$OFFSITE`（默认关闭） |
