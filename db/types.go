package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cyverse-de/p/go/ptypes"
	"github.com/cyverse-de/p/go/qms"
	"github.com/doug-martin/goqu/v9"
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

type PlanQuotaDefaultKey struct {
	ResourceTypeID string
	EffectiveDate  int64
}

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
	ID         string `db:"id" goqu:"defaultifempty"`
	Name       string `db:"name"`
	Unit       string `db:"unit"`
	Consumable bool   `db:"consumable"`
}

func (rt ResourceType) ToQMSResourceType() *qms.ResourceType {
	return &qms.ResourceType{
		Uuid:       rt.ID,
		Name:       rt.Name,
		Unit:       rt.Unit,
		Consumable: rt.Consumable,
	}
}

func NewResourceTypeFromQMS(q *qms.ResourceType) *ResourceType {
	return &ResourceType{
		ID:         q.Uuid,
		Name:       q.Name,
		Unit:       q.Unit,
		Consumable: q.Consumable,
	}
}

func (rt ResourceType) ValidateForPlan() error {

	// We must have enough information to at least attempt to look up the resource type.
	if rt.ID == "" && rt.Name == "" {
		return fmt.Errorf("either the resource type name or the resource type ID is required")
	}

	return nil
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
	Metadata        string          `db:"metadata"`
}

type Subscription struct {
	ID                 string              `db:"id" goqu:"defaultifempty"`
	EffectiveStartDate time.Time           `db:"effective_start_date"`
	EffectiveEndDate   time.Time           `db:"effective_end_date"`
	User               User                `db:"users"`
	Plan               Plan                `db:"plans"`
	Quotas             []Quota             `db:"-"`
	Usages             []Usage             `db:"-"`
	SubscriptionAddons []SubscriptionAddon `db:"-"`
	CreatedBy          string              `db:"created_by"`
	CreatedAt          time.Time           `db:"created_at" goqu:"defaultifempty"`
	LastModifiedBy     string              `db:"last_modified_by"`
	LastModifiedAt     string              `db:"last_modified_at" goqu:"defaultifempty"`
	Paid               bool                `db:"paid" goqu:"defaultifempty"`
	Rate               PlanRate            `db:"plan_rates"`
}

func NewSubscriptionFromQMS(s *qms.Subscription) *Subscription {
	var quotas []Quota
	var usages []Usage

	for _, sq := range s.Quotas {
		quotas = append(quotas, *NewQuotaFromQMS(sq))
	}
	for _, su := range s.Usages {
		usages = append(usages, *NewUsageFromQMS(su))
	}

	return &Subscription{
		ID:                 s.Uuid,
		EffectiveStartDate: s.EffectiveEndDate.AsTime(),
		EffectiveEndDate:   s.EffectiveEndDate.AsTime(),
		User: User{
			ID:       s.User.Uuid,
			Username: s.User.Username,
		},
		Quotas:         quotas,
		Usages:         usages,
		Plan:           *NewPlanFromQMS(s.Plan),
		CreatedBy:      s.User.Username,
		LastModifiedBy: s.User.Username,
		Paid:           s.Paid,
		Rate:           *NewPlanRateFromQMS(s.PlanRate, s.Plan.Uuid),
	}
}

func (up Subscription) ToQMSSubscription() *qms.Subscription {
	// Convert the list of quotas.
	quotas := make([]*qms.Quota, len(up.Quotas))
	for i, quota := range up.Quotas {
		quotas[i] = quota.ToQMSQuota()
	}

	// Convert the list of usages.
	usages := make([]*qms.Usage, len(up.Usages))
	for i, usage := range up.Usages {
		usages[i] = usage.ToQMSUsage()
	}

	// Convert the list of addons.
	addons := make([]*qms.SubscriptionAddon, len(up.SubscriptionAddons))
	for i, addon := range up.SubscriptionAddons {
		addons[i] = addon.ToQMSType()
		addons[i].SubscriptionId = ""
	}

	return &qms.Subscription{
		Uuid:               up.ID,
		EffectiveStartDate: ptypes.New(up.EffectiveStartDate),
		EffectiveEndDate:   ptypes.New(up.EffectiveEndDate),
		User:               up.User.ToQMSUser(),
		Plan:               up.Plan.ToQMSPlan(),
		Quotas:             quotas,
		Usages:             usages,
		Paid:               up.Paid,
		PlanRate:           up.Rate.ToQMSPlanRate(),
		Addons:             addons,
	}
}

type Plan struct {
	ID            string             `db:"id" goqu:"defaultifempty"`
	Name          string             `db:"name"`
	Description   string             `db:"description"`
	QuotaDefaults []PlanQuotaDefault `db:"-"`
	Rates         []PlanRate         `db:"-"`
}

func NewPlanFromQMS(q *qms.Plan) *Plan {
	pqd := make([]PlanQuotaDefault, len(q.PlanQuotaDefaults))
	for i, qd := range q.PlanQuotaDefaults {
		pqd[i] = *NewPlanQuotaDefaultFromQMS(qd, q.Uuid)
	}
	pr := make([]PlanRate, len(q.PlanRates))
	for i, r := range q.PlanRates {
		pr[i] = *NewPlanRateFromQMS(r, q.Uuid)
	}
	return &Plan{
		ID:            q.Uuid,
		Name:          q.Name,
		Description:   q.Description,
		QuotaDefaults: pqd,
		Rates:         pr,
	}
}

func (p Plan) ToQMSPlan() *qms.Plan {
	// Convert the quota defaults.
	quotaDefaults := make([]*qms.QuotaDefault, len(p.QuotaDefaults))
	for i, quotaDefault := range p.QuotaDefaults {
		quotaDefaults[i] = quotaDefault.ToQMSQuotaDefault()
	}
	rates := make([]*qms.PlanRate, len(p.Rates))
	for i, rate := range p.Rates {
		rates[i] = rate.ToQMSPlanRate()
	}

	return &qms.Plan{
		Uuid:              p.ID,
		Name:              p.Name,
		Description:       p.Description,
		PlanQuotaDefaults: quotaDefaults,
		PlanRates:         rates,
	}
}

func (p Plan) GetActiveRate() *PlanRate {
	now := time.Now()

	var effectiveRate *PlanRate
	for _, pr := range p.Rates {
		if pr.EffectiveDate.After(now) {
			break
		}
		effectiveRate = &pr
	}

	return effectiveRate
}

func (p Plan) GetActiveQuotaDefaults() []*PlanQuotaDefault {
	now := time.Now()

	pqdMap := make(map[string]*PlanQuotaDefault)
	for _, pqd := range p.QuotaDefaults {
		if pqd.EffectiveDate.After(now) {
			break
		}
		pqdMap[pqd.ResourceType.Name] = &pqd
	}

	index := 0
	pqds := make([]*PlanQuotaDefault, len(pqdMap))
	for _, pqd := range pqdMap {
		pqds[index] = pqd
		index++
	}

	return pqds
}

func (p Plan) Validate() error {

	// The plan name and description are both required.
	if p.Name == "" {
		return fmt.Errorf("a plan name is required")
	}
	if p.Description == "" {
		return fmt.Errorf("a plan description is required")
	}

	// Validate the quota defaults.
	for _, qd := range p.QuotaDefaults {
		if err := qd.ValidateForPlan(); err != nil {
			return err
		}
	}

	// Validate the plan rates.
	for _, r := range p.Rates {
		if err := r.ValidateForPlan(); err != nil {
			return err
		}
	}

	return nil
}

func (p Plan) ValidateQuotaDefaultUniqueness() error {

	// Verify that there's only one quota default per resource type and effective date.
	uniquePlanQuotaDefaults := make(map[PlanQuotaDefaultKey]bool)
	for _, qd := range p.QuotaDefaults {
		key := qd.Key()
		if uniquePlanQuotaDefaults[key] {
			return fmt.Errorf("there can only be one quota default for each resource type and effective date")
		} else {
			uniquePlanQuotaDefaults[key] = true
		}
	}

	return nil
}

func (p Plan) ValidatePlanRateUniqueness() error {

	// Verify that there's only one rate per effective date.
	uniquePlanRates := make(map[int64]bool)
	for _, r := range p.Rates {
		key := r.EffectiveDate.UnixMicro()
		if uniquePlanRates[key] {
			return fmt.Errorf("there can only be one plan rate for each effective date")
		} else {
			uniquePlanRates[key] = true
		}
	}

	return nil
}

type PlanQuotaDefault struct {
	ID            string       `db:"id" goqu:"defaultifempty"`
	PlanID        string       `db:"plan_id"`
	QuotaValue    float64      `db:"quota_value"`
	ResourceType  ResourceType `db:"resource_types"`
	EffectiveDate time.Time    `db:"effective_date"`
}

func NewPlanQuotaDefaultFromQMS(q *qms.QuotaDefault, planID string) *PlanQuotaDefault {
	var effectiveDate time.Time
	if q.EffectiveDate != nil {
		effectiveDate = q.EffectiveDate.AsTime()
	}
	return &PlanQuotaDefault{
		ID:            q.Uuid,
		PlanID:        planID,
		QuotaValue:    q.QuotaValue,
		ResourceType:  *NewResourceTypeFromQMS(q.ResourceType),
		EffectiveDate: effectiveDate,
	}
}

func (pqd PlanQuotaDefault) ToQMSQuotaDefault() *qms.QuotaDefault {
	return &qms.QuotaDefault{
		Uuid:          pqd.ID,
		QuotaValue:    pqd.QuotaValue,
		ResourceType:  pqd.ResourceType.ToQMSResourceType(),
		EffectiveDate: ptypes.New(pqd.EffectiveDate),
	}
}

func (pqd PlanQuotaDefault) ValidateForPlan() error {

	// The default quota value must be specified and greater than zero.
	if pqd.QuotaValue <= 0 {
		return fmt.Errorf("plan quota default values must be specified and greater than zero")
	}

	// The effective date must be specified.
	if pqd.EffectiveDate.IsZero() {
		return fmt.Errorf("all plan quota defaults must have an effective date")
	}

	return pqd.ResourceType.ValidateForPlan()
}

func (pqd PlanQuotaDefault) Key() PlanQuotaDefaultKey {
	return PlanQuotaDefaultKey{
		ResourceTypeID: pqd.ResourceType.ID,
		EffectiveDate:  pqd.EffectiveDate.UnixMicro(),
	}
}

type PlanRate struct {
	ID            string    `db:"id" goqu:"defaultifempty"`
	PlanID        string    `db:"plan_id"`
	EffectiveDate time.Time `db:"effective_date"`
	Rate          float64   `db:"rate"`
}

func NewPlanRateFromQMS(r *qms.PlanRate, planID string) *PlanRate {
	var effectiveDate time.Time
	if r.EffectiveDate != nil {
		effectiveDate = r.EffectiveDate.AsTime()
	}
	return &PlanRate{
		ID:            r.Uuid,
		PlanID:        planID,
		EffectiveDate: effectiveDate,
		Rate:          r.Rate,
	}
}

func (pr PlanRate) ToQMSPlanRate() *qms.PlanRate {
	return &qms.PlanRate{
		Uuid:          pr.ID,
		EffectiveDate: ptypes.New(pr.EffectiveDate),
		Rate:          pr.Rate,
	}
}

func (pr PlanRate) Validate() error {

	// The rate can't be negative.
	if pr.Rate < 0 {
		return fmt.Errorf("the plan rate must not be less than zero")
	}

	// The effective date has to be specified.
	if pr.EffectiveDate.IsZero() {
		return fmt.Errorf("the effective date of the plan rate must be specified")
	}

	return nil
}

func (pr PlanRate) ValidateForPlan() error {
	return pr.Validate()
}

type Usage struct {
	ID             string       `db:"id" goqu:"defaultifempty"`
	Usage          float64      `db:"usage"`
	SubscriptionID string       `db:"subscription_id"`
	ResourceType   ResourceType `db:"resource_types"`
	CreatedBy      string       `db:"created_by"`
	CreatedAt      time.Time    `db:"created_at"`
	LastModifiedBy string       `db:"last_modified_by"`
	LastModifiedAt time.Time    `db:"last_modified_at"`
}

func NewUsageFromQMS(q *qms.Usage) *Usage {
	return &Usage{
		ID:             q.Uuid,
		Usage:          q.Usage,
		SubscriptionID: q.SubscriptionId,
		ResourceType:   *NewResourceTypeFromQMS(q.ResourceType),
		CreatedBy:      q.CreatedBy,
		CreatedAt:      q.CreatedAt.AsTime(),
		LastModifiedBy: q.LastModifiedBy,
		LastModifiedAt: q.LastModifiedAt.AsTime(),
	}
}

func (u Usage) ToQMSUsage() *qms.Usage {
	return &qms.Usage{
		Uuid:           u.ID,
		Usage:          u.Usage,
		ResourceType:   u.ResourceType.ToQMSResourceType(),
		CreatedBy:      u.CreatedBy,
		CreatedAt:      ptypes.New(u.CreatedAt),
		LastModifiedBy: u.LastModifiedBy,
		LastModifiedAt: ptypes.New(u.LastModifiedAt),
	}
}

type Quota struct {
	ID             string       `db:"id" goqu:"defaultifempty"`
	Quota          float64      `db:"quota"`
	ResourceType   ResourceType `db:"resource_types"`
	Subscription   Subscription `db:"subscriptions"`
	CreatedBy      string       `db:"created_by"`
	CreatedAt      time.Time    `db:"created_at"`
	LastModifiedBy string       `db:"last_modified_by"`
	LastModifiedAt time.Time    `db:"last_modified_at"`
}

func NewQuotaFromQMS(q *qms.Quota) *Quota {
	return &Quota{
		ID:           q.Uuid,
		Quota:        q.Quota,
		ResourceType: *NewResourceTypeFromQMS(q.ResourceType),
		Subscription: Subscription{
			ID: q.SubscriptionId,
		},
		CreatedBy:      q.CreatedBy,
		CreatedAt:      q.CreatedAt.AsTime(),
		LastModifiedBy: q.LastModifiedBy,
		LastModifiedAt: q.LastModifiedAt.AsTime(),
	}
}

func (q Quota) ToQMSQuota() *qms.Quota {
	return &qms.Quota{
		Uuid:           q.ID,
		Quota:          q.Quota,
		ResourceType:   q.ResourceType.ToQMSResourceType(),
		CreatedBy:      q.CreatedBy,
		CreatedAt:      ptypes.New(q.CreatedAt),
		LastModifiedBy: q.LastModifiedBy,
		LastModifiedAt: ptypes.New(q.LastModifiedAt),
	}
}

type Overage struct {
	SubscriptionID string       `db:"subscription_id"`
	User           User         `db:"users"`
	Plan           Plan         `db:"plans"`
	ResourceType   ResourceType `db:"resource_types"`
	QuotaValue     float64      `db:"quota_value"`
	UsageValue     float64      `db:"usage_value"`
}

type Addon struct {
	ID            string       `db:"id" goqu:"defaultifempty,skipupdate"`
	Name          string       `db:"name"`
	Description   string       `db:"description"`
	ResourceType  ResourceType `db:"resource_types"`
	DefaultAmount float64      `db:"default_amount"`
	DefaultPaid   bool         `db:"default_paid"`
	AddonRates    []AddonRate  `db:"-"`
}

func NewAddonFromQMS(q *qms.Addon) *Addon {
	addonRates := make([]AddonRate, len(q.AddonRates))
	for i, r := range q.AddonRates {
		addonRates[i] = *NewAddonRateFromQMS(r, q.Uuid)
	}
	return &Addon{
		Name:          q.Name,
		Description:   q.Description,
		DefaultAmount: q.DefaultAmount,
		DefaultPaid:   q.DefaultPaid,
		ResourceType:  *NewResourceTypeFromQMS(q.ResourceType),
		AddonRates:    addonRates,
	}
}

func (a *Addon) ToQMSType() *qms.Addon {
	addonRates := make([]*qms.AddonRate, len(a.AddonRates))
	for i, r := range a.AddonRates {
		addonRates[i] = r.ToQMSType()
	}
	return &qms.Addon{
		Uuid:          a.ID,
		Name:          a.Name,
		Description:   a.Description,
		DefaultAmount: a.DefaultAmount,
		DefaultPaid:   a.DefaultPaid,
		ResourceType: &qms.ResourceType{
			Uuid:       a.ResourceType.ID,
			Name:       a.ResourceType.Name,
			Unit:       a.ResourceType.Unit,
			Consumable: a.ResourceType.Consumable,
		},
		AddonRates: addonRates,
	}
}

func (a *Addon) Validate() error {

	// The name and description are both required.
	if a.Name == "" {
		return fmt.Errorf("name must be set")
	}
	if a.Description == "" {
		return fmt.Errorf("description must be set")
	}

	// The default amount must be positive.
	if a.DefaultAmount <= 0.0 {
		return fmt.Errorf("default_amount must be greater than 0.0")
	}

	// Verify that we have enough information to attempt to look up the resource type.
	if err := a.ResourceType.ValidateForPlan(); err != nil {
		return err
	}

	// Validate the incoming addon rates.
	return nil
}

func (a *Addon) ValidateAddonRateUniqueness() error {

	// Verify that there's only one rate per effective date.
	uniqueAddonRates := make(map[int64]bool)
	for _, r := range a.AddonRates {
		key := r.EffectiveDate.UnixMicro()
		if uniqueAddonRates[key] {
			return fmt.Errorf("there can only be one plan rate for each effective date")
		} else {
			uniqueAddonRates[key] = true
		}
	}

	return nil
}

// GetCurrentRate determines the rate that is currently active.
func (a *Addon) GetCurrentRate() *AddonRate {
	var currentRate *AddonRate
	now := time.Now()
	for _, r := range a.AddonRates {
		if r.EffectiveDate.After(now) {
			break
		}
		currentRate = &r
	}
	return currentRate
}

type AddonRate struct {
	ID            string    `db:"id" goqu:"defaultifempty,skipupdate"`
	AddonID       string    `db:"addon_id"`
	EffectiveDate time.Time `db:"effective_date"`
	Rate          float64   `db:"rate"`
}

func NewAddonRateFromQMS(r *qms.AddonRate, addonID string) *AddonRate {
	var effectiveDate time.Time
	if r.EffectiveDate != nil {
		effectiveDate = r.EffectiveDate.AsTime()
	}
	return &AddonRate{
		ID:            r.Uuid,
		AddonID:       addonID,
		EffectiveDate: effectiveDate,
		Rate:          r.Rate,
	}
}

func (r *AddonRate) ToQMSType() *qms.AddonRate {
	return &qms.AddonRate{
		Uuid:          r.ID,
		EffectiveDate: ptypes.New(r.EffectiveDate),
		Rate:          r.Rate,
	}
}

func (r *AddonRate) ToRec() goqu.Record {
	var rec = map[string]any{
		"addon_id":       r.AddonID,
		"effective_date": r.EffectiveDate,
		"rate":           r.Rate,
	}
	if r.ID != "" {
		rec["id"] = r.ID
	}
	return goqu.Record(rec)
}

func (r *AddonRate) Validate() error {

	// The rate can't be negative.
	if r.Rate < 0 {
		return fmt.Errorf("the plan rate must not be less than zero")
	}

	// The effective date has to be specified.
	if r.EffectiveDate.IsZero() {
		return fmt.Errorf("the effective date of the plan rate must be specified")
	}

	return nil
}

type UpdateAddon struct {
	ID                  string      `db:"id" goqu:"skipupdate"`
	Name                string      `db:"name"`
	UpdateName          bool        `db:"-"`
	Description         string      `db:"description"`
	UpdateDescription   bool        `db:"-"`
	ResourceTypeID      string      `db:"resource_type_id"`
	UpdateResourceType  bool        `db:"-"`
	DefaultAmount       float64     `db:"default_amount"`
	UpdateDefaultAmount bool        `db:"-"`
	DefaultPaid         bool        `db:"default_paid"`
	UpdateDefaultPaid   bool        `db:"-"`
	AddonRates          []AddonRate `db:"-"`
	UpdateAddonRates    bool        `db:"-"`
}

func NewUpdateAddonFromQMS(u *qms.UpdateAddonRequest) *UpdateAddon {
	update := &UpdateAddon{
		ID:                  u.Addon.Uuid,
		UpdateName:          u.UpdateName,
		UpdateDescription:   u.UpdateDescription,
		UpdateResourceType:  u.UpdateResourceType,
		UpdateDefaultAmount: u.UpdateDefaultAmount,
		UpdateDefaultPaid:   u.UpdateDefaultPaid,
		UpdateAddonRates:    u.UpdateAddonRates,
	}

	if update.UpdateName {
		update.Name = u.Addon.Name
	}
	if update.UpdateDescription {
		update.Description = u.Addon.Description
	}
	if update.UpdateDefaultAmount {
		update.DefaultAmount = float64(u.Addon.DefaultAmount)
	}
	if update.UpdateDefaultPaid {
		update.DefaultPaid = u.Addon.DefaultPaid
	}
	if update.UpdateResourceType {
		update.ResourceTypeID = u.Addon.ResourceType.Uuid
	}
	if update.UpdateAddonRates {
		addonRates := make([]AddonRate, len(u.Addon.AddonRates))
		for i, r := range u.Addon.AddonRates {
			addonRates[i] = *NewAddonRateFromQMS(r, u.Addon.Uuid)
		}
		update.AddonRates = addonRates
	}
	return update
}

type SubscriptionAddon struct {
	ID             string    `db:"id" goqu:"defaultifempty,skipupdate"`
	Addon          Addon     `db:"addons"`
	SubscriptionID string    `db:"subscription_id"`
	Amount         float64   `db:"amount"`
	Paid           bool      `db:"paid"`
	Rate           AddonRate `db:"addon_rates"`
}

func NewSubscriptionAddonFromQMS(sa *qms.SubscriptionAddon) *SubscriptionAddon {
	return &SubscriptionAddon{
		ID:             sa.Uuid,
		Addon:          *NewAddonFromQMS(sa.Addon),
		SubscriptionID: sa.SubscriptionId,
		Amount:         float64(sa.Amount),
		Paid:           sa.Paid,
		Rate:           *NewAddonRateFromQMS(sa.AddonRate, sa.Uuid),
	}
}

func (sa *SubscriptionAddon) ToQMSType() *qms.SubscriptionAddon {
	return &qms.SubscriptionAddon{
		Uuid:           sa.ID,
		Addon:          sa.Addon.ToQMSType(),
		SubscriptionId: sa.SubscriptionID,
		Amount:         sa.Amount,
		Paid:           sa.Paid,
		AddonRate:      sa.Rate.ToQMSType(),
	}
}

type UpdateSubscriptionAddon struct {
	ID                   string  `db:"id" goqu:"skipupdate"`
	AddonID              string  `db:"addon_id"`
	UpdateAddonID        bool    `db:"-"`
	SubscriptionID       string  `db:"subscription_id"`
	UpdateSubscriptionID bool    `db:"-"`
	Amount               float64 `db:"amount"`
	UpdateAmount         bool    `db:"-"`
	Paid                 bool    `db:"paid"`
	UpdatePaid           bool    `db:"-"`
}

// NewUpdateSubscriptoinAddonFromQMS does what the name implies. A caveat is that
// The UpdateAddonID and UpdateSubscriptionID fields are ignored by the logic in
// the service.
func NewUpdateSubscriptionAddonFromQMS(q *qms.UpdateSubscriptionAddonRequest) *UpdateSubscriptionAddon {
	update := &UpdateSubscriptionAddon{
		ID:                   q.SubscriptionAddon.Uuid,
		UpdateAddonID:        q.UpdateAddonId,
		UpdateSubscriptionID: q.UpdateSubscriptionId,
		UpdateAmount:         q.UpdateAmount,
		UpdatePaid:           q.UpdatePaid,
	}
	if update.UpdateAmount {
		update.Amount = float64(q.SubscriptionAddon.Amount)
	}
	if update.UpdatePaid {
		update.Paid = q.SubscriptionAddon.Paid
	}
	return update
}
