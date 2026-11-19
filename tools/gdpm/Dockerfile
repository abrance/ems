# gdpm 日采集镜像：官方披露 API -> ems 业务库 gd_spot_metric。
# 抓取与解析仍用标准库（urllib/csv/json）；入库支持 PG（psycopg）与 sqlite 两种。
FROM python:3.12-slim

# 抓取要 HTTPS 到源站；slim 里没有 ca-certificates 时 urlopen 直接报证书错。
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# 2026-11-19 ems 关系库切 PostgreSQL：写 PG 需要驱动，sqlite 分支仍用标准库。
RUN pip install --no-cache-dir "psycopg[binary]>=3.2,<4"

WORKDIR /app
# gd_pm_tree.json 是披露节点树缓存：带上就少一次 /nav/tree 请求，也避免只读镜像层写缓存失败。
COPY gd_pm_fetch.py gdpm_dataset.py gdpm_sqlite.py gdpm_daily.py gdpm_db.py \
     gd_pm_index.json gd_pm_tree.json /app/
COPY entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/entrypoint.sh \
 && mkdir -p /app/out \
 && useradd --create-home --uid 10001 gdpm \
 && chown -R gdpm:gdpm /app/out
USER gdpm

# 抓取中间文件落 /tmp（容器内可写）；库固定 /app/out/db.sqlite3，由外部挂载。
ENV GDPM_DB=/app/out/db.sqlite3 \
    GDPM_WORK_DIR=/tmp/gdpm \
    PYTHONUNBUFFERED=1

ENTRYPOINT ["/app/entrypoint.sh"]
