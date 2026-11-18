#!/bin/sh
# ems 线上 API 冒烟/测试脚本：用静态 token 调接口，按响应体的 code 决定退出码。
#
#   EMS_TOKEN=xxx ./ems_api.sh GET "/api/spot-report?page=1"
#   EMS_BASE=http://127.0.0.1:8080 EMS_TOKEN=xxx ./ems_api.sh POST "/api/xxx"
#   EMS_AUTH=bearer EMS_TOKEN=xxx ./ems_api.sh GET "/api/health"
#
# 退出码 = 响应体里的 code（0 成功）。shell 只会保留 code & 0xFF，401 表现为 145，
# 所以失败时以 stderr 上的 "code=… message=…" 那一行为准。用法/网络错误为 2/3/curl 自身的码。
# 详见 docs/api-static-token.md。
set -eu

BASE="${EMS_BASE:-}"
TOKEN="${EMS_TOKEN:-}"

if [ -z "$BASE" ]; then
	echo "EMS_BASE 未设置，拒绝发送请求（脚本内不含任何默认地址）。" >&2
	exit 2
fi

if [ "$#" -lt 2 ]; then
	echo "用法: EMS_BASE=<地址> EMS_TOKEN=<token> $0 <METHOD> <PATH> [额外 curl 参数...]" >&2
	echo "  EMS_BASE  目标 base（必填）" >&2
	echo "  EMS_AUTH  header（默认，X-API-Token）| bearer（Authorization: Bearer）" >&2
	echo "token 只从环境变量 EMS_TOKEN 读取，脚本内不含任何默认值。" >&2
	exit 2
fi

METHOD="$1"
shift
REQPATH="$1"
shift

if [ -z "$TOKEN" ]; then
	echo "EMS_TOKEN 未设置，拒绝发送请求。" >&2
	exit 2
fi

case "${EMS_AUTH:-header}" in
header) AUTH_HEADER="X-API-Token: ${TOKEN}" ;;
bearer) AUTH_HEADER="Authorization: Bearer ${TOKEN}" ;;
*)
	echo "EMS_AUTH 只能是 header 或 bearer，实际是 ${EMS_AUTH}" >&2
	exit 2
	;;
esac

URL="${BASE}${REQPATH}"

set +e
RESPONSE="$(curl -sS -X "$METHOD" -H "$AUTH_HEADER" "$@" "$URL")"
CURL_RC=$?
set -e
if [ "$CURL_RC" -ne 0 ]; then
	echo "请求失败（curl exit ${CURL_RC}）: ${METHOD} ${URL}" >&2
	exit "$CURL_RC"
fi

# 取响应体里第一个 "code"（GoFrame 统一响应把 code/message/data 放在最外层）。
CODE="$(printf '%s' "$RESPONSE" \
	| grep -o '"code"[[:space:]]*:[[:space:]]*-\{0,1\}[0-9][0-9]*' \
	| head -n 1 \
	| sed 's/.*:[[:space:]]*//')"

if [ -z "$CODE" ]; then
	echo "响应体里找不到 code 字段，原始响应:" >&2
	printf '%s\n' "$RESPONSE" >&2
	exit 3
fi

if [ "$CODE" -ne 0 ]; then
	MESSAGE="$(printf '%s' "$RESPONSE" \
		| grep -o '"message"[[:space:]]*:[[:space:]]*"[^"]*"' \
		| head -n 1 \
		| sed 's/^[^:]*:[[:space:]]*"//; s/"$//')"
	echo "code=${CODE} message=${MESSAGE}" >&2
	exit "$CODE"
fi

printf '%s\n' "$RESPONSE"
