# gdpm 日采集镜像：官方披露 API -> ems 业务库 gd_spot_metric。
# 纯标准库（urllib/csv/sqlite3/json），没有任何 pip 依赖，所以不装包也不放 requirements。
FROM python:3.12-slim

# 抓取要 HTTPS 到 pm.gd.csg.cn；slim 里没有 ca-certificates 时 urlopen 直接报证书错。
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
# gd_pm_tree.json 是披露节点树缓存：带上就少一次 /nav/tree 请求，也避免只读镜像层写缓存失败。
COPY gd_pm_fetch.py gdpm_dataset.py gdpm_sqlite.py gdpm_daily.py \
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
