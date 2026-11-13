package homemarket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"s_db"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/frame/g"
)

// 采集状态（run 表 + 状态端点）的端到端测试：真 sqlite + http 桩。
//
// 守的是四件只有跑真库/真流程才看得见的事：
//  1. 失败必须**只**写 run 表 —— 数据表多一行 0 或前一日的复制值，页面就会
//     把「源站挂了」显示成「今天价格是 0」；
//  2. 「HTTP 200 + 空 payload」与「失败」是两种状态，前者不能报错；
//  3. 四个非正常态各自能被现算出来（状态不写库，改口径不受历史脏值影响）；
//  4. 回补互斥：第二个调用立刻拿到冲突错误，而不是悄悄并发跑或 panic。

type runRow struct {
	TargetDate          string `orm:"target_date"`
	Dataset             string `orm:"dataset"`
	Status              string `orm:"status"`
	ErrorKind           string `orm:"error_kind"`
	RowsWritten         int    `orm:"rows_written"`
	AttemptCount        int    `orm:"attempt_count"`
	ConsecutiveFailures int    `orm:"consecutive_failures"`
	ConsecutiveEmpties  int    `orm:"consecutive_empties"`
	LastError           string `orm:"last_error"`
}

func fetchRunRows(t *testing.T, dataset string) []runRow {
	t.Helper()
	var rows []runRow
	err := g.DB().GetScan(context.Background(), &rows,
		`SELECT target_date, dataset, status, error_kind, rows_written, attempt_count,
		        consecutive_failures, consecutive_empties, coalesce(last_error,'') AS last_error
		   FROM gd_home_market_fetch_run WHERE dataset = ? ORDER BY target_date DESC`, dataset)
	if err != nil {
		t.Fatalf("查 run 表失败: %v", err)
	}
	return rows
}

func countRows(t *testing.T, table string) int {
	t.Helper()
	v, err := g.DB().GetValue(context.Background(), "SELECT count(*) FROM "+table)
	if err != nil {
		t.Fatalf("统计 %s 失败: %v", table, err)
	}
	return v.Int()
}

// blockAll 源站被动态防护挡住：HTTP 400（真实形态），每次请求都失败。
func blockAll(t *testing.T) {
	t.Helper()
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusBadRequest)
	})
}

// emptyAll 请求成功但 payload 明确为空：源站「还没发布」，不是失败。
func emptyAll(t *testing.T) {
	t.Helper()
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"msg":"操作成功！","data":{"details":[]}}`)
	})
}

// okAll 每次请求都返回 24 点。
func okAll(t *testing.T) {
	t.Helper()
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"msg":"ok","data":{"details":[`)
		for h := 0; h < 24; h++ {
			if h > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"powerNum":%d,"currentPrice":%d,"realTimePrice":%d,"dateTime":"%s %02d:00:00"}`,
				60000+h, 400+h, 500+h, r.URL.Query().Get("date"), h)
		}
		fmt.Fprint(w, `]}}`)
	})
}

// ① 14 天全失败：run 行必须记成 failed + error_kind=blocked，且数据表**一行都不能多**。
func TestCollectAllFailuresRecordsRunOnlyAndWritesNoData(t *testing.T) {
	newTestDB(t)
	blockAll(t)

	if _, _, err := CollectHome24h(context.Background(), time.Now()); err == nil {
		t.Fatal("14 天全 400 时应返回 error")
	} else if got := classifyErr(err); got != ErrKindBlocked {
		t.Errorf("错误类别 = %q，期望 %q", got, ErrKindBlocked)
	}

	rows := fetchRunRows(t, s_db_model.DatasetHome24h)
	if len(rows) != maxLookbackDays {
		t.Fatalf("run 行数 = %d，期望 %d（每探一天一行）", len(rows), maxLookbackDays)
	}
	for _, r := range rows {
		if r.Status != s_db_model.FetchStatusFailed {
			t.Errorf("%s 状态 = %q，期望 %q", r.TargetDate, r.Status, s_db_model.FetchStatusFailed)
		}
		if r.ErrorKind != string(ErrKindBlocked) {
			t.Errorf("%s error_kind = %q，期望 %q", r.TargetDate, r.ErrorKind, ErrKindBlocked)
		}
		if r.RowsWritten != 0 {
			t.Errorf("%s 记了 %d 行写入，失败时应为 0", r.TargetDate, r.RowsWritten)
		}
		if r.LastError == "" {
			t.Errorf("%s 没记 last_error，排障时看不出原因", r.TargetDate)
		}
	}
	// 关键断言：失败不写数据表（不写 0、不复制前一日）
	if got := countRows(t, s_db_model.TableHome24h); got != 0 {
		t.Fatalf("失败时数据表写了 %d 行，期望 0 行（宁可没数，也不要 0）", got)
	}
	// 也没写地市表
	if got := countRows(t, s_db_model.TableNodePrice); got != 0 {
		t.Fatalf("24 点失败却写了 %d 行地市数据", got)
	}
}

// ② 200 + 空 payload：记成 empty，且不报错（这是「源站未发布」，不是故障）。
func TestCollectEmptyPayloadRecordsEmptyStatusWithoutError(t *testing.T) {
	newTestDB(t)
	emptyAll(t)

	date, n, err := CollectHome24h(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("空 payload 不该报错: %v", err)
	}
	if date != "" || n != 0 {
		t.Errorf("未发布时应返回空日期/0 点: date=%q n=%d", date, n)
	}
	rows := fetchRunRows(t, s_db_model.DatasetHome24h)
	if len(rows) != maxLookbackDays {
		t.Fatalf("run 行数 = %d，期望 %d", len(rows), maxLookbackDays)
	}
	for _, r := range rows {
		if r.Status != s_db_model.FetchStatusEmpty {
			t.Errorf("%s 状态 = %q，期望 %q", r.TargetDate, r.Status, s_db_model.FetchStatusEmpty)
		}
		if r.ErrorKind != "" || r.LastError != "" {
			t.Errorf("%s 为空不该带错误信息: kind=%q err=%q", r.TargetDate, r.ErrorKind, r.LastError)
		}
	}
	if got := countRows(t, s_db_model.TableHome24h); got != 0 {
		t.Fatalf("空 payload 时数据表写了 %d 行，期望 0 行", got)
	}
}

// ③ 四个非正常态各出一条：never / failed / lagging / stopped。
func TestMarketStatusDerivesFourStates(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.Local)
	fmtDate := func(daysAgo int) string { return now.AddDate(0, 0, -daysAgo).Format(dateLayout) }

	cases := []struct {
		name  string
		seed  func(t *testing.T) // 造数据：数据表 + run 表
		want  string
		check func(t *testing.T, st DatasetStatus)
	}{
		{
			name: "never",
			seed: func(t *testing.T) {}, // 库是空的：从没成功过，也没有任何数据
			want: StateNever,
			check: func(t *testing.T, st DatasetStatus) {
				if st.AsOf != "" || st.LagDays != -1 || st.Rows != 0 {
					t.Errorf("never 应 asOf=空 lagDays=-1 rows=0，实际 %+v", st)
				}
			},
		},
		{
			name: "failed",
			seed: func(t *testing.T) {
				seed24h(t, fmtDate(6), 24) // 库里有更早的数
				if _, err := s_db.GetHomeMarketService().RecordFetchRun(context.Background(), &s_db_model.SFetchRunModel{
					TargetDate: fmtDate(0), Dataset: s_db_model.DatasetHome24h,
					Status: s_db_model.FetchStatusFailed, ErrorKind: string(ErrKindBlocked),
					RowsExpected: rowsExpectedHome24h, LastError: "blocked: HTTP 400（blocked）",
				}); err != nil {
					t.Fatalf("写 run 失败: %v", err)
				}
			},
			want: StateFailed,
			check: func(t *testing.T, st DatasetStatus) {
				if st.ErrorKind != string(ErrKindBlocked) || st.LastError == "" {
					t.Errorf("failed 应带 errorKind/lastError，实际 %+v", st)
				}
				if st.AsOf != fmtDate(6) || st.LagDays != 6 {
					t.Errorf("failed 仍应报出最新有数日与滞后：%+v", st)
				}
			},
		},
		{
			name: "lagging",
			seed: func(t *testing.T) {
				// 滞后 20 天 > 24 点的预期上限 8 天：采集在跑，但没采到新的
				seed24h(t, fmtDate(20), 24)
				if _, err := s_db.GetHomeMarketService().RecordFetchRun(context.Background(), &s_db_model.SFetchRunModel{
					TargetDate: fmtDate(20), Dataset: s_db_model.DatasetHome24h,
					Status: s_db_model.FetchStatusOK, RowsWritten: 24, RowsExpected: rowsExpectedHome24h,
				}); err != nil {
					t.Fatalf("写 run 失败: %v", err)
				}
			},
			want: StateLagging,
			check: func(t *testing.T, st DatasetStatus) {
				if st.LagDays != 20 {
					t.Errorf("lagDays = %d，期望 20", st.LagDays)
				}
				if st.FetchedAt == "" {
					t.Error("lagging 应给出上次成功时间 fetchedAt")
				}
				// *gtime.Time.Format 是 PHP 风格："Y-m-d H:i:s" 才是真日期，
				// 传 Go 的 layout（"2006-01-02 15:04:05"）会被当成字面量原样吐出来。
				// 两种写法都不会报错，只是页面印出一串假日期，所以这里两个都钉。
				for _, layout := range []string{"2006-01-02 15:04:05", "Y-m-d H:i:s"} {
					if st.FetchedAt == layout {
						t.Errorf("fetchedAt = %q，格式串被原样输出了（*gtime.Time.Format 要 PHP 风格 Y-m-d H:i:s）", layout)
					}
				}
				if st.Rows != 24 || st.RowsExpected != 24 {
					t.Errorf("rows/rowsExpected = %d/%d，期望 24/24", st.Rows, st.RowsExpected)
				}
			},
		},
		{
			// 全新环境：库里一条数都没有（asOf 为空），源站连续回 200+空。
			// 必须报 stopped，不能报 never —— 前者说「源站改版了」，后者说「你配置没配好」。
			name: "stopped_beats_never_on_empty_db",
			seed: func(t *testing.T) {
				for i := 0; i < stoppedEmptyRounds; i++ {
					if _, err := s_db.GetHomeMarketService().RecordFetchRun(context.Background(), &s_db_model.SFetchRunModel{
						TargetDate: fmtDate(0), Dataset: s_db_model.DatasetHome24h,
						Status: s_db_model.FetchStatusEmpty, RowsExpected: rowsExpectedHome24h,
					}); err != nil {
						t.Fatalf("写 run 失败: %v", err)
					}
				}
			},
			want: StateStopped,
			check: func(t *testing.T, st DatasetStatus) {
				if st.AsOf != "" {
					t.Errorf("这个用例库里不该有数据，asOf = %q", st.AsOf)
				}
			},
		},
		{
			name: "stopped",
			seed: func(t *testing.T) {
				seed24h(t, fmtDate(6), 24)
				// 连续 K 次「200 + 空」：源站停更/改版
				for i := 0; i < stoppedEmptyRounds; i++ {
					if _, err := s_db.GetHomeMarketService().RecordFetchRun(context.Background(), &s_db_model.SFetchRunModel{
						TargetDate: fmtDate(0), Dataset: s_db_model.DatasetHome24h,
						Status: s_db_model.FetchStatusEmpty, RowsExpected: rowsExpectedHome24h,
					}); err != nil {
						t.Fatalf("写 run 失败: %v", err)
					}
				}
			},
			want: StateStopped,
			check: func(t *testing.T, st DatasetStatus) {
				if st.LagDays != 6 {
					t.Errorf("lagDays = %d，期望 6（按今天算，不受查询参数影响）", st.LagDays)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newTestDB(t)
			c.seed(t)
			st, err := LoadMarketStatus(context.Background(), now)
			if err != nil {
				t.Fatalf("LoadMarketStatus 失败: %v", err)
			}
			if st.Home24h.State != c.want {
				t.Fatalf("state = %q，期望 %q（%+v）", st.Home24h.State, c.want, st.Home24h)
			}
			if st.Home24h.Dataset != s_db_model.DatasetHome24h {
				t.Errorf("dataset = %q，期望 %q", st.Home24h.Dataset, s_db_model.DatasetHome24h)
			}
			c.check(t, st.Home24h)
		})
	}
}

// ④ 连续失败递增，再成功一轮归零并刷新 last_ok_at。
func TestConsecutiveFailuresResetOnSuccess(t *testing.T) {
	newTestDB(t)
	now := time.Now()
	today := now.Format(dateLayout)

	blockAll(t)
	for i := 0; i < 2; i++ {
		if _, _, err := CollectHome24h(context.Background(), now); err == nil {
			t.Fatal("源站 400 时应返回 error")
		}
	}
	rows := fetchRunRows(t, s_db_model.DatasetHome24h)
	if len(rows) == 0 {
		t.Fatal("失败后 run 表为空")
	}
	pick := func(rows []runRow, date string) *runRow {
		for i := range rows {
			if rows[i].TargetDate == date {
				return &rows[i]
			}
		}
		return nil
	}
	todayRow := pick(rows, today)
	if todayRow == nil {
		t.Fatalf("run 表里没有今天的行: %+v", rows)
	}
	if todayRow.ConsecutiveFailures != 2 {
		t.Fatalf("连续失败 = %d，期望 2", todayRow.ConsecutiveFailures)
	}
	if todayRow.AttemptCount != 2 {
		t.Fatalf("尝试次数 = %d，期望 2", todayRow.AttemptCount)
	}

	if v, err := g.DB().GetValue(context.Background(),
		`SELECT last_ok_at FROM gd_home_market_fetch_run WHERE dataset=? AND target_date=?`,
		s_db_model.DatasetHome24h, today); err != nil {
		t.Fatalf("查 last_ok_at 失败: %v", err)
	} else if v.IsNil() || v.String() == "" {
		// 从没成功过必须是 NULL，不能是 0001-01-01
	} else {
		t.Fatalf("从没成功过，last_ok_at 应为 NULL，实际 %v", v.String())
	}

	// 换成一个「都发布」的源站：再来一轮应当成功
	okAll(t)
	date, n, err := CollectHome24h(context.Background(), now)
	if err != nil {
		t.Fatalf("成功轮不该报错: %v", err)
	}
	if n != 24 {
		t.Fatalf("写入点数 = %d，期望 24", n)
	}
	todayRow = pick(fetchRunRows(t, s_db_model.DatasetHome24h), date)
	if todayRow == nil {
		t.Fatalf("run 表里没有 %s 的行", date)
	}
	if todayRow.Status != s_db_model.FetchStatusOK {
		t.Errorf("状态 = %q，期望 %q", todayRow.Status, s_db_model.FetchStatusOK)
	}
	if todayRow.ConsecutiveFailures != 0 {
		t.Errorf("成功后连续失败没归零: %d", todayRow.ConsecutiveFailures)
	}
	v, err := g.DB().GetValue(context.Background(),
		`SELECT last_ok_at FROM gd_home_market_fetch_run WHERE dataset=? AND target_date=?`,
		s_db_model.DatasetHome24h, date)
	if err != nil {
		t.Fatalf("查 last_ok_at 失败: %v", err)
	}
	if v.IsNil() || v.String() == "" {
		t.Error("成功后 last_ok_at 应被写上有值")
	}
}

// ⑤ 回补互斥：锁被占时第二个调用立刻拿到冲突错误（不 panic、不静默并发）。
func TestRunBackfillRejectsConcurrentCall(t *testing.T) {
	newTestDB(t)
	newLagStub(t)
	ctx := context.Background()
	now := time.Now()

	backfillMu.Lock()
	home24h, node, err := RunBackfill(ctx, now, defaultBackfillDays)
	backfillMu.Unlock()

	if !errors.Is(err, ErrBackfillBusy) {
		t.Fatalf("锁被占时应返回 ErrBackfillBusy，实际 %v", err)
	}
	if len(home24h) != 0 || len(node) != 0 {
		t.Errorf("冲突时不该返回任何写入结果: %v %v", home24h, node)
	}
	if got := countRows(t, s_db_model.TableFetchRun); got != 0 {
		t.Errorf("冲突时不该发起任何请求，run 表却有 %d 行", got)
	}

	// 锁释放后能正常跑完（证明是互斥而不是永久卡死）
	if _, _, err := RunBackfill(ctx, now, defaultBackfillDays); err != nil {
		t.Fatalf("锁释放后回补失败: %v", err)
	}
}
