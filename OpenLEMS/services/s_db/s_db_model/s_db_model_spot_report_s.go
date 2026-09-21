package s_db_model

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

const (
	TableSpotReport            = "gd_spot_report"
	FieldSpotReportRunDate     = "run_date"
	FieldSpotReportTitle       = "title"
	FieldSpotReportSourceURL   = "source_url"
	FieldSpotReportPages       = "pages"
	FieldSpotReportCollectedAt = "collected_at"
)

type SSpotReportModel struct {
	g.Meta      `orm:"table:gd_spot_report"`
	Id          int         `json:"id" orm:"id,primary,auto_increment"`
	RunDate     string      `json:"runDate" orm:"run_date"`
	Title       string      `json:"title" orm:"title"`
	SourceURL   string      `json:"sourceUrl" orm:"source_url"`
	Pages       string      `json:"pages" orm:"pages"`
	CollectedAt *gtime.Time `json:"collectedAt" orm:"collected_at"`
	CreatedAt   *gtime.Time `json:"createdAt" orm:"created_at,created_at"`
	UpdatedAt   *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}

func (m *SSpotReportModel) Create(ctx context.Context) error {
	_, err := g.Model(TableSpotReport).Ctx(ctx).FieldsEx(FieldId).Insert(m)
	return err
}

func (m *SSpotReportModel) GetByDate(ctx context.Context, runDate string) error {
	return g.Model(TableSpotReport).Ctx(ctx).Where(FieldSpotReportRunDate, runDate).Scan(m)
}

func (m *SSpotReportModel) Update(ctx context.Context) error {
	_, err := g.Model(TableSpotReport).Ctx(ctx).
		Where(FieldId, m.Id).
		FieldsEx(FieldId, FieldCreatedAt).
		Update(m)
	return err
}
