package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/cyverse-de/p/go/qms"
	"github.com/doug-martin/goqu/v9"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type GoquDatabase interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	From(cols ...interface{}) *goqu.SelectDataset
	Insert(table interface{}) *goqu.InsertDataset
	Prepare(query string) (*sql.Stmt, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ScanStruct(i interface{}, query string, args ...interface{}) (bool, error)
	ScanStructContext(ctx context.Context, i interface{}, query string, args ...interface{}) (bool, error)
	ScanStructs(i interface{}, query string, args ...interface{}) error
	ScanStructsContext(ctx context.Context, i interface{}, query string, args ...interface{}) error
	ScanVal(i interface{}, query string, args ...interface{}) (bool, error)
	ScanValContext(ctx context.Context, i interface{}, query string, args ...interface{}) (bool, error)
	ScanVals(i interface{}, query string, args ...interface{}) error
	ScanValsContext(ctx context.Context, i interface{}, query string, args ...interface{}) error
	Select(cols ...interface{}) *goqu.SelectDataset
	Trace(op, sqlString string, args ...interface{})
	Truncate(table ...interface{}) *goqu.TruncateDataset
	Update(table interface{}) *goqu.UpdateDataset
}

type SQLStatement interface {
	ToSQL() (sql string, params []interface{}, err error)
}

var CurrentTimestamp = goqu.L("CURRENT_TIMESTAMP")

var UpdateOperationNames = []string{"ADD", "SET"}

const UsagesTrackedMetric = "usages"
const QuotasTrackedMetric = "quotas"

const DefaultPlanName = "Basic"

type UpdateOperation struct {
	ID   string `db:"id" goqu:"defaultifempty"`
	Name string `db:"name"`
}

type User struct {
	ID       string `db:"id" goqu:"defaultifempty"`
	Username string `db:"username"`
}

func (u User) ToQMSUser() *qms.QMSUser {
	return &qms.QMSUser{
		Uuid:     u.ID,
		Username: u.Username,
	}
}

type ResourceType struct {
	ID   string `db:"id" goqu:"defaultifempty"`
	Name string `db:"name"`
	Unit string `db:"unit"`
}

func (rt ResourceType) ToQMSResourceType() *qms.ResourceType {
	return &qms.ResourceType{
		Uuid: rt.ID,
		Name: rt.Name,
		Unit: rt.Unit,
	}
}

var ResourceTypeNames = []string{
	"cpu.hours",
	"data.size",
}

var ResourceTypeUnits = []string{
	"cpu hours",
	"bytes",
}

const (
	UpdateTypeSet = "SET"
	UpdateTypeAdd = "ADD"
)

type Update struct {
	ID              string          `db:"id" goqu:"defaultifempty"`
	ValueType       string          `db:"value_type"`
	Value           float64         `db:"value"`
	EffectiveDate   time.Time       `db:"effective_date"`
	CreatedBy       string          `db:"created_by"`
	CreatedAt       time.Time       `db:"created_at" goqu:"defaultifempty"`
	LastModifiedBy  string          `db:"last_modified_by"`
	LastModifiedAt  time.Time       `db:"last_modified_at" goqu:"defaultifempty"`
	ResourceType    ResourceType    `db:"resource_types"`
	User            User            `db:"users"`
	UpdateOperation UpdateOperation `db:"update_operations"`
}

type UserPlan struct {
	ID                 string    `db:"id" goqu:"defaultifempty"`
	EffectiveStartDate time.Time `db:"effective_start_date"`
	EffectiveEndDate   time.Time `db:"effective_end_date"`
	User               User      `db:"users"`
	Plan               Plan      `db:"plans"`
	Quotas             []Quota   `db:"-"`
	Usages             []Usage   `db:"-"`
	CreatedBy          string    `db:"created_by"`
	CreatedAt          time.Time `db:"created_at" goqu:"defaultifempty"`
	LastModifiedBy     string    `db:"last_modified_by"`
	LastModifiedAt     string    `db:"last_modified_at" goqu:"defaultifempty"`
}

func (up UserPlan) ToQMSUserPlan() *qms.UserPlan {
	// Convert the list of quotas.
	quotas := make([]*qms.Quota, len(up.Quotas))
	for i, quota := range up.Quotas {
		quotas[i] = quota.ToQMSQuota()
	}

	// Convert th elist of usages.
	usages := make([]*qms.Usage, len(up.Usages))
	for i, usage := range up.Usages {
		usages[i] = usage.ToQMSUsage()
	}

	return &qms.UserPlan{
		Uuid:               up.ID,
		EffectiveStartDate: timestamppb.New(up.EffectiveStartDate),
		EffectiveEndDate:   timestamppb.New(up.EffectiveEndDate),
		User:               up.User.ToQMSUser(),
		Plan:               up.Plan.ToQMSPlan(),
		Quotas:             quotas,
		Usages:             usages,
	}
}

type Plan struct {
	ID            string             `db:"id" goqu:"defaultifempty"`
	Name          string             `db:"name"`
	Description   string             `db:"description"`
	QuotaDefaults []PlanQuotaDefault `db:"-"`
}

func (p Plan) ToQMSPlan() *qms.Plan {
	// Convert the quota defaults.
	quotaDefaults := make([]*qms.QuotaDefault, len(p.QuotaDefaults))
	for i, quotaDefault := range p.QuotaDefaults {
		quotaDefaults[i] = quotaDefault.ToQMSQuotaDefault()
	}

	return &qms.Plan{
		Uuid:              p.ID,
		Name:              p.Name,
		Description:       p.Description,
		PlanQuotaDefaults: quotaDefaults,
	}
}

type PlanQuotaDefault struct {
	ID           string       `db:"id" goqu:"defaultifempty"`
	PlanID       string       `db:"plan_id"`
	QuotaValue   float64      `db:"quota_value"`
	ResourceType ResourceType `db:"resource_types"`
}

func (pqd PlanQuotaDefault) ToQMSQuotaDefault() *qms.QuotaDefault {
	return &qms.QuotaDefault{
		Uuid:         pqd.ID,
		QuotaValue:   float32(pqd.QuotaValue),
		ResourceType: pqd.ResourceType.ToQMSResourceType(),
	}
}

type Usage struct {
	ID             string       `db:"id" goqu:"defaultifempty"`
	Usage          float64      `db:"usage"`
	UserPlanID     string       `db:"user_plan_id"`
	ResourceType   ResourceType `db:"resource_types"`
	CreatedBy      string       `db:"created_by"`
	CreatedAt      time.Time    `db:"created_at"`
	LastModifiedBy string       `db:"last_modified_by"`
	LastModifiedAt time.Time    `db:"last_modified_at"`
}

func (u Usage) ToQMSUsage() *qms.Usage {
	return &qms.Usage{
		Uuid:           u.ID,
		Usage:          u.Usage,
		ResourceType:   u.ResourceType.ToQMSResourceType(),
		CreatedBy:      u.CreatedBy,
		CreatedAt:      timestamppb.New(u.CreatedAt),
		LastModifiedBy: u.LastModifiedBy,
		LastModifiedAt: timestamppb.New(u.LastModifiedAt),
	}
}

type Quota struct {
	ID             string       `db:"id" goqu:"defaultifempty"`
	Quota          float64      `db:"quota"`
	ResourceType   ResourceType `db:"resource_types"`
	UserPlan       UserPlan     `db:"user_plans"`
	CreatedBy      string       `db:"created_by"`
	CreatedAt      time.Time    `db:"created_at"`
	LastModifiedBy string       `db:"last_modified_by"`
	LastModifiedAt time.Time    `db:"last_modified_at"`
}

func (q Quota) ToQMSQuota() *qms.Quota {
	return &qms.Quota{
		Uuid:           q.ID,
		Quota:          float32(q.Quota),
		ResourceType:   q.ResourceType.ToQMSResourceType(),
		CreatedBy:      q.CreatedBy,
		CreatedAt:      timestamppb.New(q.CreatedAt),
		LastModifiedBy: q.LastModifiedBy,
		LastModifiedAt: timestamppb.New(q.LastModifiedAt),
	}
}

type Overage struct {
	UserPlanID   string       `db:"user_plan_id"`
	User         User         `db:"users"`
	Plan         Plan         `db:"plans"`
	ResourceType ResourceType `db:"resource_types"`
	QuotaValue   float64      `db:"quota_value"`
	UsageValue   float64      `db:"usage_value"`
}
