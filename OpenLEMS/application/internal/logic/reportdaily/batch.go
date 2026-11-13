package reportdaily

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	v1 "application/api/reportdaily/v1"
	"s_db"
)

// 区间批量解析：解析器升级后历史日报不会自己更新，需要按区间重跑。
//
// 串行执行（与 spotreport/batch.go 同一语义）：一篇日报要跑 4 页 OCR，OCR 服务只有
// 2 个并发槽，并发重跑会把宿主机 CPU 打满。接口立即返回任务 id，前端轮询进度。
const batchJobKeep = 5

type batchJob struct {
	mu         sync.Mutex
	Id         string
	Status     string // running / done / failed
	Total      int
	Done       int
	Succeeded  int
	Failed     int
	MetricRows int
	Preserved  int
	Current    string
	Errors     []string
}

type batchRegistry struct {
	mu   sync.Mutex
	jobs map[string]*batchJob
	last string
}

var batchJobs = &batchRegistry{jobs: map[string]*batchJob{}}

// StartBatch 起一个后台批量解析任务。
//
// 默认只解析有日报档案、且该日还没有任何日报原值（或 includeManual=true）的日期：
// 已有 OCR 行的日子重跑没有收益，还会白白烧 OCR 配额。
func StartBatch(ctx context.Context, from, to string, includeManual, dryRun bool) (*v1.BatchExtractRes, error) {
	reports, err := s_db.GetSpotReportService().List(ctx)
	if err != nil {
		return nil, err
	}
	var planned, skipped []string
	for _, r := range reports {
		if from != "" && r.RunDate < from {
			continue
		}
		if to != "" && r.RunDate > to {
			continue
		}
		planned = append(planned, r.RunDate)
	}
	sort.Strings(planned)
	if len(planned) == 0 {
		return nil, fmt.Errorf("指定区间内没有已采集的日报")
	}

	if !includeManual {
		latest, err := s_db.GetReportDailyService().LatestRunDate(ctx)
		if err != nil {
			return nil, err
		}
		if latest != "" {
			// 已经解析到 latest 的日报不再重跑（重跑只会把 OCR 结果原样再写一遍）
			kept := planned[:0]
			for _, d := range planned {
				if d <= latest {
					skipped = append(skipped, d+"（已有日报原值，跳过）")
					continue
				}
				kept = append(kept, d)
			}
			planned = kept
		}
	}
	if dryRun {
		return &v1.BatchExtractRes{Status: "planned", Total: len(planned), Dates: planned, Skipped: nonNilStrings(skipped)}, nil
	}
	if len(planned) == 0 {
		return nil, fmt.Errorf("区间内 %d 篇日报都已有日报原值，无需重跑", len(skipped))
	}

	job := &batchJob{Id: batchJobId(), Status: "running", Total: len(planned), Errors: []string{}}
	batchJobs.mu.Lock()
	batchJobs.jobs[job.Id] = job
	batchJobs.last = job.Id
	pruneJobsLocked()
	batchJobs.mu.Unlock()

	go runBatch(context.WithoutCancel(ctx), job, planned, includeManual)
	return &v1.BatchExtractRes{Id: job.Id, Status: "running", Total: len(planned), Dates: planned, Skipped: nonNilStrings(skipped)}, nil
}

func runBatch(ctx context.Context, job *batchJob, dates []string, includeManual bool) {
	for _, date := range dates {
		job.mu.Lock()
		job.Current = date
		job.mu.Unlock()

		res, err := ExtractDay(ctx, date, includeManual)
		job.mu.Lock()
		job.Done++
		if err != nil {
			job.Failed++
			job.Errors = append(job.Errors, date+"："+err.Error())
		} else {
			job.Succeeded++
			job.MetricRows += res.MetricRows
			job.Preserved += res.Preserved
		}
		job.mu.Unlock()
	}
	job.mu.Lock()
	job.Status = "done"
	job.Current = ""
	job.mu.Unlock()
}

// GetBatchStatus 读任务进度；id 为空取最近一次。
func GetBatchStatus(id string) (*v1.BatchStatusRes, error) {
	batchJobs.mu.Lock()
	if id == "" {
		id = batchJobs.last
	}
	job := batchJobs.jobs[id]
	batchJobs.mu.Unlock()
	if job == nil {
		return nil, fmt.Errorf("任务不存在或已被清理：%s", id)
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	return &v1.BatchStatusRes{
		Id: job.Id, Status: job.Status, Total: job.Total, Done: job.Done,
		Succeeded: job.Succeeded, Failed: job.Failed,
		MetricRows: job.MetricRows, Preserved: job.Preserved,
		Current: job.Current, Errors: nonNilStrings(job.Errors),
	}, nil
}

func batchJobId() string {
	return fmt.Sprintf("rd-%d", time.Now().UnixNano())
}

func pruneJobsLocked() {
	if len(batchJobs.jobs) <= batchJobKeep {
		return
	}
	ids := make([]string, 0, len(batchJobs.jobs))
	for id := range batchJobs.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids[:len(ids)-batchJobKeep] {
		delete(batchJobs.jobs, id)
	}
}
