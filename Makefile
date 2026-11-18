# 本地 dev 统一入口：固定端口，重复执行先清理旧进程，避免留下无人管理的实例。
ROOT := $(CURDIR)
DEV_DIR := $(ROOT)/.dev
API_PID := $(DEV_DIR)/api.pid
WEB_PID := $(DEV_DIR)/web.pid
API_LOG := $(DEV_DIR)/api.log
WEB_LOG := $(DEV_DIR)/web.log

.PHONY: dev dev-stop dev-status dev-logs

## 启动/重启本地 dev：backend :15880, frontend :5173
dev: dev-stop
	@mkdir -p $(DEV_DIR)
	@echo "Starting backend  : http://127.0.0.1:15880 (log: $(API_LOG))"
	@setsid -w sh -c 'cd $(ROOT)/OpenLEMS/application && exec go run . --web=true --demo=true --driver-path=resources/driver --profile=dev' \
		> $(API_LOG) 2>&1 < /dev/null & echo $$! > $(API_PID)
	@echo "Starting frontend : http://127.0.0.1:5173 (log: $(WEB_LOG))"
	@setsid -w sh -c 'cd $(ROOT)/OpenLEMS/web && exec npm run dev' \
		> $(WEB_LOG) 2>&1 < /dev/null & echo $$! > $(WEB_PID)
	@for i in $$(seq 1 30); do \
		if ss -ltn | rg -q ':15880 ' && ss -ltn | rg -q ':5173 '; then break; fi; \
		sleep 1; \
	done
	@$(MAKE) --no-print-directory dev-status

## 停止本地 dev：按 .dev/*.pid 的进程组杀，再兜底清固定端口
dev-stop:
	@for f in $(API_PID) $(WEB_PID); do \
		if [ -f "$$f" ]; then \
			pid=$$(cat "$$f"); \
			kill -TERM -$$pid 2>/dev/null || kill -TERM $$pid 2>/dev/null || true; \
			rm -f "$$f"; \
		fi; \
	done
	@sleep 1
	@fuser -k -n tcp 15880 >/dev/null 2>&1 || true
	@fuser -k -n tcp 5173 >/dev/null 2>&1 || true
	@echo "dev stopped (ports 15880/5173 free)"

## 查看固定端口占用
dev-status:
	@ss -ltnp | rg ':(15880|5173) ' || echo "dev ports not listening"

## 同时跟两个日志
dev-logs:
	@tail -F -n 40 $(API_LOG) $(WEB_LOG)
