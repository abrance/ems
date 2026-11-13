package spotreport

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	v1 "application/api/spotreport/v1"
	"s_db"
)

// 批量重跑解析：解析器/映射逻辑升级后，历史日报不会自动更新，需要按区间重跑。
//
// 每篇日报要跑 4 页 OCR（约 6-10 秒）再加地图采样，所以做成后台任务：
// 接口立即返回任务 id，前端轮询进度。任务串行执行——OCR 服务只有 2 个并发槽，
// 并发重跑会把宿主机 CPU 打满，也会让 lems 自身变慢。
//
// 语义与单篇一致（以 OCR 为准）：重跑会覆盖该日报已人工确认/修订的值，
// 覆盖前写审计（operation=overwritten_by_extract），并计入 Overwritten 计数，
// 前端在启动前会明确提示这一点。
const batchJobKeep = 5

type batchJob struct {
	mu          sync.Mutex
	Id          string
	Status      string // running / done / failed
	Total       int
	Done        int
	Succeeded   int
	Failed      int
	Overwritten int
	Skipped     []string
	Current     string
	Errors      []string
	StartedAt   time.Time
	FinishedAt  time.Time
}

type batchRegistry struct {
	mu   sync.Mutex
	jobs map[string]*batchJob
	last string
}

var batchJobs = &batchRegistry{jobs: map[string]*batchJob{}}

// StartBatchExtract 起一个后台批量重跑任务。
//
// 默认跳过「已全部人工复核」的日报：批量重跑的本意是解析器升级后回填历史，
// 顺手清掉别人已复核的状态是意外副作用（实测发生过：一次批量重置了 24 项确认，
// 值虽未变但复核状态丢失）。需要覆盖时显式传 IncludeConfirmed——它同时也是
// 单篇解析的 force：只对 ocr/cv 生效，官方直采与人工确认值不会被覆盖。
func StartBatchExtract(ctx context.Context, from, to string, includeConfirmed, dryRun bool) (*v1.BatchExtractRes, error) {
	all, err := collectedDatesInRange(ctx, from, to)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("指定区间内没有已采集的日报")
	}
	dates, skipped, err := planBatch(ctx, all, includeConfirmed)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return &v1.BatchExtractRes{
			Total: len(dates), Status: "planned",
			Dates: dates, SkippedConfirmed: skipped,
		}, nil
	}
	if len(dates) == 0 {
		return nil, fmt.Errorf("区间内 %d 篇日报都已人工复核，无需重跑；已复核值不会被覆盖", len(skipped))
	}

	batchJobs.mu.Lock()
	if last := batchJobs.jobs[batchJobs.last]; last != nil {
		last.mu.Lock()
		running := last.Status == "running"
		last.mu.Unlock()
		if running {
			batchJobs.mu.Unlock()
			return nil, fmt.Errorf("已有一个批量任务在执行（%s），请等它结束", batchJobs.last)
		}
	}
	id := fmt.Sprintf("batch-%d", time.Now().Unix())
	job := &batchJob{Id: id, Status: "running", Total: len(dates), Skipped: skipped, StartedAt: time.Now()}
	batchJobs.jobs[id] = job
	batchJobs.last = id
	batchJobs.trimLocked()
	batchJobs.mu.Unlock()

	// 与 HTTP 请求解耦：请求返回后任务继续跑
	go runBatchJob(job, dates, includeConfirmed)

	return &v1.BatchExtractRes{Id: id, Total: len(dates), Status: job.Status,
		Dates: dates, SkippedConfirmed: skipped}, nil
}

// GetBatchExtract 查询任务进度；id 为空时返回最近一次任务
func GetBatchExtract(id string) (*v1.BatchExtractStatusRes, error) {
	batchJobs.mu.Lock()
	if id == "" {
		id = batchJobs.last
	}
	job := batchJobs.jobs[id]
	batchJobs.mu.Unlock()
	if job == nil {
		return nil, fmt.Errorf("未找到批量任务")
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	res := &v1.BatchExtractStatusRes{
		Id: job.Id, Status: job.Status, Total: job.Total, Done: job.Done,
		Succeeded: job.Succeeded, Failed: job.Failed, Overwritten: job.Overwritten,
		Current: job.Current, Errors: nonNilStrings(append([]string{}, job.Errors...)),
		SkippedConfirmed: nonNilStrings(append([]string{}, job.Skipped...)),
		StartedAt:        job.StartedAt.In(shanghaiLocation()).Format(time.RFC3339),
	}
	if !job.FinishedAt.IsZero() {
		res.FinishedAt = job.FinishedAt.In(shanghaiLocation()).Format(time.RFC3339)
	}
	return res, nil
}

func runBatchJob(job *batchJob, dates []string, includeConfirmed bool) {
	ctx := context.Background()
	for _, date := range dates {
		job.mu.Lock()
		job.Current = date
		job.mu.Unlock()

		res, err := ExtractReport(ctx, date, includeConfirmed)
		job.mu.Lock()
		job.Done++
		if err != nil {
			job.Failed++
			if len(job.Errors) < 20 {
				job.Errors = append(job.Errors, fmt.Sprintf("%s：%v", date, err))
			}
		} else {
			job.Succeeded++
			job.Overwritten += countOverwritten(res.Warnings)
		}
		job.mu.Unlock()
	}
	job.mu.Lock()
	job.Current = ""
	job.Status = "done"
	if job.Succeeded == 0 {
		job.Status = "failed"
	}
	job.FinishedAt = time.Now()
	job.mu.Unlock()
}

// countOverwritten 从告警里数出「重跑覆盖人工值」的条数（ExtractReport 逐条产出）
func countOverwritten(warnings []string) int {
	count := 0
	for _, w := range warnings {
		if strings.HasPrefix(w, OverwriteWarningPrefix) {
			count++
		}
	}
	return count
}

// planBatch 把已采集日期分成「本次重跑」与「跳过（已全部人工复核）」
func planBatch(ctx context.Context, all []string, includeConfirmed bool) (dates, skipped []string, err error) {
	dates = make([]string, 0, len(all))
	skipped = make([]string, 0)
	for _, date := range all {
		if includeConfirmed {
			dates = append(dates, date)
			continue
		}
		metrics, err := s_db.GetSpotExtractService().ListMetrics(ctx, date)
		if err != nil {
			return nil, nil, err
		}
		pending, confirmed := CountReview(metrics)
		if skipConfirmedReport(pending, confirmed) {
			skipped = append(skipped, date)
			continue
		}
		dates = append(dates, date)
	}
	return dates, skipped, nil
}

// skipConfirmedReport 是否跳过：只有「全部已确认」才跳过。
// 只要还有未复核项（ocr 或 cv）就说明没复核完，不算已复核；
// 全无人工确认状态（未解析）也没有可丢失的复核状态，可以重跑。
//
// 这里不能用「没有 cv 就算复核完」——把 ocr 也算作未复核后，
// 一篇 58 项 ocr + 24 项 manual 的日报会被误判为已复核而跳过重跑。
//
// 注：ReplaceMetrics 现在默认就不覆盖已确认行，这里的跳过是第二道护栏——
// 全部确认的日报连 OCR 都不必重跑，也避免给已确认值产生无意义的重算告警。
func skipConfirmedReport(pending, confirmed int) bool {
	return pending == 0 && confirmed > 0
}

// collectedDatesInRange 取区间内已采集的日报日（升序）；from/to 为空表示不限
func collectedDatesInRange(ctx context.Context, from, to string) ([]string, error) {
	models, err := s_db.GetSpotReportService().List(ctx)
	if err != nil {
		return nil, err
	}
	dates := make([]string, 0, len(models))
	for _, m := range models {
		if from != "" && m.RunDate < from {
			continue
		}
		if to != "" && m.RunDate > to {
			continue
		}
		dates = append(dates, m.RunDate)
	}
	sort.Strings(dates)
	return dates, nil
}

func (r *batchRegistry) trimLocked() {
	if len(r.jobs) <= batchJobKeep {
		return
	}
	ids := make([]string, 0, len(r.jobs))
	for id := range r.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids[:len(ids)-batchJobKeep] {
		delete(r.jobs, id)
	}
}
