package model

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Match modes
const (
	ModelQuotaMatchModeExact  = "exact"
	ModelQuotaMatchModePrefix = "prefix"
)

const (
	ModelQuotaScopeModel = "model"
	ModelQuotaScopeAll   = "all"
)

// Rule sources
const (
	ModelQuotaRuleSourceGroup = "group"
	ModelQuotaRuleSourcePlan  = "plan"
	ModelQuotaRuleSourceUser  = "user"
)

// Period types for group rules (plan rules always follow subscription cycle)
const (
	ModelQuotaPeriodTotal   = "total"   // 总额限制（不重置）
	ModelQuotaPeriodDaily   = "daily"   // 每天重置
	ModelQuotaPeriodWeekly  = "weekly"  // 每周重置（周一）
	ModelQuotaPeriodMonthly = "monthly" // 每月重置（1号）
)

// Usage status
const (
	ModelQuotaUsageStatusActive  = "active"
	ModelQuotaUsageStatusExpired = "expired"
)

// ---------------------------------------------------------------------------
// ModelQuotaGroupRule — 分组级规则定义
// ---------------------------------------------------------------------------

type ModelQuotaGroupRule struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	GroupName    string `json:"group_name" gorm:"column:group_name;type:varchar(64);not null;index:idx_group_rules"`
	Scope        string `json:"scope" gorm:"column:scope;type:varchar(16);not null;default:'model'"`
	ModelPattern string `json:"model_pattern" gorm:"column:model_pattern;type:varchar(128);not null"`
	MatchMode    string `json:"match_mode" gorm:"column:match_mode;type:varchar(16);not null;default:'exact'"`
	Period       string `json:"period" gorm:"column:period;type:varchar(16);not null;default:'total'"`
	QuotaLimit   int64  `json:"quota_limit" gorm:"column:quota_limit;type:bigint;not null;default:0"`
	TokenLimit   int64  `json:"token_limit" gorm:"column:token_limit;type:bigint;not null;default:0"`
	Enabled      bool   `json:"enabled" gorm:"column:enabled;index:idx_group_rules"`
	SortOrder    int    `json:"sort_order" gorm:"column:sort_order;type:int;default:0"`
	CreatedAt    int64  `json:"created_at" gorm:"column:created_at;type:bigint"`
	UpdatedAt    int64  `json:"updated_at" gorm:"column:updated_at;type:bigint"`
}

func (r *ModelQuotaGroupRule) BeforeCreate(tx *gorm.DB) error {
	if r.Scope == "" {
		r.Scope = ModelQuotaScopeModel
	}
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *ModelQuotaGroupRule) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func (r *ModelQuotaGroupRule) TableName() string {
	return "model_quota_group_rules"
}

// GetModelQuotaGroupRulesByGroup returns all enabled rules for a given group, ordered by sort_order
func GetModelQuotaGroupRulesByGroup(groupName string) ([]*ModelQuotaGroupRule, error) {
	var rules []*ModelQuotaGroupRule
	err := DB.Where("group_name = ? AND enabled = ?", groupName, true).
		Order("sort_order ASC, id ASC").
		Find(&rules).Error
	return rules, err
}

// ---------------------------------------------------------------------------
// ModelQuotaPlanRule — 套餐级规则定义
// ---------------------------------------------------------------------------

type ModelQuotaPlanRule struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	PlanId       int    `json:"plan_id" gorm:"column:plan_id;type:int;not null;index:idx_plan_rules"`
	Scope        string `json:"scope" gorm:"column:scope;type:varchar(16);not null;default:'model'"`
	ModelPattern string `json:"model_pattern" gorm:"column:model_pattern;type:varchar(128);not null"`
	MatchMode    string `json:"match_mode" gorm:"column:match_mode;type:varchar(16);not null;default:'exact'"`
	QuotaLimit   int64  `json:"quota_limit" gorm:"column:quota_limit;type:bigint;not null;default:0"`
	TokenLimit   int64  `json:"token_limit" gorm:"column:token_limit;type:bigint;not null;default:0"`
	Enabled      bool   `json:"enabled" gorm:"column:enabled;index:idx_plan_rules"`
	SortOrder    int    `json:"sort_order" gorm:"column:sort_order;type:int;default:0"`
	CreatedAt    int64  `json:"created_at" gorm:"column:created_at;type:bigint"`
	UpdatedAt    int64  `json:"updated_at" gorm:"column:updated_at;type:bigint"`
}

func (r *ModelQuotaPlanRule) BeforeCreate(tx *gorm.DB) error {
	if r.Scope == "" {
		r.Scope = ModelQuotaScopeModel
	}
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *ModelQuotaPlanRule) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func (r *ModelQuotaPlanRule) TableName() string {
	return "model_quota_plan_rules"
}

// GetModelQuotaPlanRulesByPlanId returns all enabled rules for a given plan, ordered by sort_order
func GetModelQuotaPlanRulesByPlanId(planId int) ([]*ModelQuotaPlanRule, error) {
	var rules []*ModelQuotaPlanRule
	err := DB.Where("plan_id = ? AND enabled = ?", planId, true).
		Order("sort_order ASC, id ASC").
		Find(&rules).Error
	return rules, err
}

// ---------------------------------------------------------------------------
// ModelQuotaUserRule — 个人用户级规则定义
// ---------------------------------------------------------------------------

type ModelQuotaUserRule struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	UserId       int    `json:"user_id" gorm:"column:user_id;type:int;not null;index:idx_user_rules"`
	Username     string `json:"username" gorm:"column:username;type:varchar(64);not null;default:''"` // 冗余字段，方便 admin 列表展示
	Scope        string `json:"scope" gorm:"column:scope;type:varchar(16);not null;default:'model'"`
	ModelPattern string `json:"model_pattern" gorm:"column:model_pattern;type:varchar(128);not null"`
	MatchMode    string `json:"match_mode" gorm:"column:match_mode;type:varchar(16);not null;default:'exact'"`
	Period       string `json:"period" gorm:"column:period;type:varchar(16);not null;default:'total'"`
	QuotaLimit   int64  `json:"quota_limit" gorm:"column:quota_limit;type:bigint;not null;default:0"`
	TokenLimit   int64  `json:"token_limit" gorm:"column:token_limit;type:bigint;not null;default:0"`
	Enabled      bool   `json:"enabled" gorm:"column:enabled;index:idx_user_rules"`
	SortOrder    int    `json:"sort_order" gorm:"column:sort_order;type:int;default:0"`
	CreatedAt    int64  `json:"created_at" gorm:"column:created_at;type:bigint"`
	UpdatedAt    int64  `json:"updated_at" gorm:"column:updated_at;type:bigint"`
}

func (r *ModelQuotaUserRule) BeforeCreate(tx *gorm.DB) error {
	if r.Scope == "" {
		r.Scope = ModelQuotaScopeModel
	}
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *ModelQuotaUserRule) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func (r *ModelQuotaUserRule) TableName() string {
	return "model_quota_user_rules"
}

// GetModelQuotaUserRulesByUserId returns all enabled rules for a given user, ordered by sort_order
func GetModelQuotaUserRulesByUserId(userId int) ([]*ModelQuotaUserRule, error) {
	var rules []*ModelQuotaUserRule
	err := DB.Where("user_id = ? AND enabled = ?", userId, true).
		Order("sort_order ASC, id ASC").
		Find(&rules).Error
	return rules, err
}

// ---------------------------------------------------------------------------
// UserModelQuotaUsage — 用户级实时消耗计数器
// ---------------------------------------------------------------------------

type UserModelQuotaUsage struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	UserId         int    `json:"user_id" gorm:"column:user_id;type:int;not null;index:idx_user_period,priority:1;uniqueIndex:idx_usage_identity,priority:1"`
	RuleId         int    `json:"rule_id" gorm:"column:rule_id;type:int;not null;uniqueIndex:idx_usage_identity,priority:3"`
	RuleSource     string `json:"rule_source" gorm:"column:rule_source;type:varchar(16);not null;default:'group';uniqueIndex:idx_usage_identity,priority:2"`
	ModelPattern   string `json:"model_pattern" gorm:"column:model_pattern;type:varchar(128);not null"`
	SubscriptionId int    `json:"subscription_id" gorm:"column:subscription_id;type:int;default:0;uniqueIndex:idx_usage_identity,priority:4"`
	QuotaLimit     int64  `json:"quota_limit" gorm:"column:quota_limit;type:bigint;not null;default:0"`
	QuotaUsed      int64  `json:"quota_used" gorm:"column:quota_used;type:bigint;not null;default:0"`
	TokenLimit     int64  `json:"token_limit" gorm:"column:token_limit;type:bigint;not null;default:0"`
	TokenUsed      int64  `json:"token_used" gorm:"column:token_used;type:bigint;not null;default:0"`
	PeriodStart    int64  `json:"period_start" gorm:"column:period_start;type:bigint;uniqueIndex:idx_usage_identity,priority:5"`
	PeriodEnd      int64  `json:"period_end" gorm:"column:period_end;type:bigint;index:idx_user_period,priority:2;uniqueIndex:idx_usage_identity,priority:6"`
	Status         string `json:"status" gorm:"column:status;type:varchar(16);not null;default:'active';index:idx_user_period,priority:3"`
	CreatedAt      int64  `json:"created_at" gorm:"column:created_at;type:bigint"`
	UpdatedAt      int64  `json:"updated_at" gorm:"column:updated_at;type:bigint"`
}

func (u *UserModelQuotaUsage) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	u.CreatedAt = now
	u.UpdatedAt = now
	return nil
}

func (u *UserModelQuotaUsage) BeforeUpdate(tx *gorm.DB) error {
	u.UpdatedAt = common.GetTimestamp()
	return nil
}

func (u *UserModelQuotaUsage) TableName() string {
	return "user_model_quota_usage"
}

func initializeModelQuotaRuleScopes() error {
	for _, table := range []any{&ModelQuotaGroupRule{}, &ModelQuotaPlanRule{}, &ModelQuotaUserRule{}} {
		if err := DB.Model(table).Where("scope = ? OR scope IS NULL", "").Update("scope", ModelQuotaScopeModel).Error; err != nil {
			return err
		}
	}
	return nil
}

// mergeDuplicateModelQuotaUsage prepares historical data before AutoMigrate
// creates the usage identity unique index.
func mergeDuplicateModelQuotaUsage() error {
	if !DB.Migrator().HasTable(&UserModelQuotaUsage{}) {
		return nil
	}

	type identity struct {
		UserId         int
		RuleId         int
		RuleSource     string
		SubscriptionId int
		PeriodStart    int64
		PeriodEnd      int64
	}
	type historicalUsage struct {
		Id             int
		UserId         int
		RuleId         int
		RuleSource     string
		SubscriptionId int
		PeriodStart    int64
		PeriodEnd      int64
		QuotaUsed      int64
		Status         string
	}

	var rows []historicalUsage
	if err := DB.Table("user_model_quota_usage").
		Select("id, user_id, rule_id, rule_source, subscription_id, period_start, period_end, quota_used, status").
		Order("id ASC").Scan(&rows).Error; err != nil {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		canonical := make(map[identity]*historicalUsage, len(rows))
		for i := range rows {
			row := &rows[i]
			key := identity{
				UserId: row.UserId, RuleId: row.RuleId, RuleSource: row.RuleSource,
				SubscriptionId: row.SubscriptionId, PeriodStart: row.PeriodStart, PeriodEnd: row.PeriodEnd,
			}
			first, exists := canonical[key]
			if !exists {
				canonical[key] = row
				continue
			}
			if (row.QuotaUsed > 0 && first.QuotaUsed > math.MaxInt64-row.QuotaUsed) ||
				(row.QuotaUsed < 0 && first.QuotaUsed < math.MinInt64-row.QuotaUsed) {
				return fmt.Errorf("model quota usage overflow while merging duplicate records %d and %d", first.Id, row.Id)
			}
			first.QuotaUsed += row.QuotaUsed
			if row.Status == ModelQuotaUsageStatusActive {
				first.Status = ModelQuotaUsageStatusActive
			}
			if err := tx.Model(&UserModelQuotaUsage{}).Where("id = ?", first.Id).
				Updates(map[string]any{"quota_used": first.QuotaUsed, "status": first.Status}).Error; err != nil {
				return err
			}
			if err := tx.Delete(&UserModelQuotaUsage{}, row.Id).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetActiveUserModelQuotaUsage returns all active, non-expired usage records for a user
func GetActiveUserModelQuotaUsage(userId int) ([]*UserModelQuotaUsage, error) {
	var usages []*UserModelQuotaUsage
	now := common.GetTimestamp()
	err := DB.Where("user_id = ? AND status = ? AND period_end > ?", userId, ModelQuotaUsageStatusActive, now).
		Find(&usages).Error
	return usages, err
}

// GetUserModelQuotaUsageByUserAndRule returns the active, non-expired usage for a specific user+rule combination.
// If the existing usage's period has ended, it returns nil to signal the caller to create a new one.
func GetUserModelQuotaUsageByUserAndRule(userId int, ruleId int, ruleSource string) (*UserModelQuotaUsage, error) {
	var usage UserModelQuotaUsage
	now := common.GetTimestamp()
	err := DB.Where("user_id = ? AND rule_id = ? AND rule_source = ? AND status = ? AND period_end > ?",
		userId, ruleId, ruleSource, ModelQuotaUsageStatusActive, now).
		First(&usage).Error
	if err != nil {
		return nil, err
	}
	return &usage, nil
}

// ExpireOutdatedUserModelQuotaUsage marks an existing usage as expired (period ended).
func ExpireOutdatedUserModelQuotaUsage(userId int, ruleId int, ruleSource string) error {
	now := common.GetTimestamp()
	return DB.Model(&UserModelQuotaUsage{}).
		Where("user_id = ? AND rule_id = ? AND rule_source = ? AND status = ? AND period_end <= ?",
			userId, ruleId, ruleSource, ModelQuotaUsageStatusActive, now).
		UpdateColumn("status", ModelQuotaUsageStatusExpired).Error
}

// GetUserModelQuotaUsageByUserId returns all usage records (including expired) for a user
func GetUserModelQuotaUsageByUserId(userId int) ([]*UserModelQuotaUsage, error) {
	var usages []*UserModelQuotaUsage
	err := DB.Where("user_id = ?", userId).
		Order("status DESC, updated_at DESC").
		Find(&usages).Error
	return usages, err
}

// IncreaseUserModelQuotaUsage atomically increments both usage metrics.
func IncreaseUserModelQuotaUsage(usageId int, quotaDelta, tokenDelta int64) error {
	if quotaDelta < 0 || tokenDelta < 0 {
		return fmt.Errorf("usage delta must be non-negative: quota=%d tokens=%d", quotaDelta, tokenDelta)
	}
	if err := DB.Model(&UserModelQuotaUsage{}).Where("id = ?", usageId).Updates(map[string]any{
		"quota_used": gorm.Expr("CASE WHEN quota_used > ? THEN ? ELSE quota_used + ? END", math.MaxInt64-quotaDelta, int64(math.MaxInt64), quotaDelta),
		"token_used": gorm.Expr("CASE WHEN token_used > ? THEN ? ELSE token_used + ? END", math.MaxInt64-tokenDelta, int64(math.MaxInt64), tokenDelta),
		"updated_at": common.GetTimestamp(),
	}).Error; err != nil {
		return err
	}
	if err := CacheIncrModelQuotaUsage(usageId, quotaDelta, tokenDelta); err != nil {
		CacheDeleteModelQuotaUsage(usageId)
		common.SysError(fmt.Sprintf("usage %d persisted but cache update failed: %v", usageId, err))
	}
	return nil
}

func AdjustTaskModelQuotaUsage(usageIds []int, quotaDelta int64) error {
	if len(usageIds) == 0 || quotaDelta == 0 {
		return nil
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, usageId := range usageIds {
			var usage UserModelQuotaUsage
			if err := lockForUpdate(tx).Where("id = ?", usageId).First(&usage).Error; err != nil {
				return err
			}
			if quotaDelta > 0 && usage.QuotaUsed > math.MaxInt64-quotaDelta {
				return fmt.Errorf("task usage adjustment overflow for usage %d", usageId)
			}
			newUsed := usage.QuotaUsed + quotaDelta
			if newUsed < 0 {
				newUsed = 0
			}
			if err := tx.Model(&UserModelQuotaUsage{}).Where("id = ?", usageId).Updates(map[string]any{
				"quota_used": newUsed,
				"updated_at": common.GetTimestamp(),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, usageId := range usageIds {
		CacheDeleteModelQuotaUsage(usageId)
	}
	return nil
}

func increaseModelQuotaUsageDB(usageId int, delta int64) error {
	return DB.Model(&UserModelQuotaUsage{}).
		Where("id = ?", usageId).
		UpdateColumn("quota_used", gorm.Expr("quota_used + ?", delta)).Error
}

// ResetUserModelQuotaUsage resets both usage metrics to 0.
func ResetUserModelQuotaUsage(usageId int) error {
	// Invalidate Redis cache
	CacheDeleteModelQuotaUsage(usageId)

	return DB.Model(&UserModelQuotaUsage{}).
		Where("id = ?", usageId).
		Updates(map[string]any{"quota_used": 0, "token_used": 0, "updated_at": common.GetTimestamp()}).Error
}

// ExpireUserModelQuotaUsage marks a usage record as expired
func ExpireUserModelQuotaUsage(usageId int) error {
	CacheDeleteModelQuotaUsage(usageId)

	return DB.Model(&UserModelQuotaUsage{}).
		Where("id = ?", usageId).
		UpdateColumn("status", ModelQuotaUsageStatusExpired).Error
}

// DeleteUserModelQuotaUsageByRule deletes all usage records associated with a
// given rule (by rule_id + rule_source) and clears their Redis caches.
// Called when a group/plan rule is deleted to prevent stale snapshots from
// blocking users.
func DeleteUserModelQuotaUsageByRule(ruleId int, ruleSource string) error {
	// Collect usage IDs first so we can clear Redis caches
	var usages []*UserModelQuotaUsage
	if err := DB.Where("rule_id = ? AND rule_source = ?", ruleId, ruleSource).
		Find(&usages).Error; err != nil {
		return err
	}
	for _, u := range usages {
		CacheDeleteModelQuotaUsage(u.Id)
	}
	return DB.Where("rule_id = ? AND rule_source = ?", ruleId, ruleSource).
		Delete(&UserModelQuotaUsage{}).Error
}

func SyncUserModelQuotaLimitsByRule(ruleId int, ruleSource string, quotaLimit, tokenLimit int64) error {
	var usages []*UserModelQuotaUsage
	if err := DB.Where("rule_id = ? AND rule_source = ? AND status = ?", ruleId, ruleSource, ModelQuotaUsageStatusActive).
		Find(&usages).Error; err != nil {
		return err
	}
	if err := DB.Model(&UserModelQuotaUsage{}).
		Where("rule_id = ? AND rule_source = ? AND status = ?", ruleId, ruleSource, ModelQuotaUsageStatusActive).
		Updates(map[string]any{"quota_limit": quotaLimit, "token_limit": tokenLimit, "updated_at": common.GetTimestamp()}).Error; err != nil {
		return err
	}
	for _, u := range usages {
		u.QuotaLimit = quotaLimit
		u.TokenLimit = tokenLimit
		_ = CacheSetModelQuotaUsage(u.Id, ModelQuotaUsageSnapshot{
			QuotaUsed:  u.QuotaUsed,
			QuotaLimit: quotaLimit,
			TokenUsed:  u.TokenUsed,
			TokenLimit: tokenLimit,
		}, u.PeriodEnd)
	}
	return nil
}

func ArchiveUserModelQuotaUsageByRule(ruleId int, ruleSource string) error {
	var usages []*UserModelQuotaUsage
	if err := DB.Where("rule_id = ? AND rule_source = ? AND status = ?", ruleId, ruleSource, ModelQuotaUsageStatusActive).
		Find(&usages).Error; err != nil {
		return err
	}
	if err := DB.Model(&UserModelQuotaUsage{}).
		Where("rule_id = ? AND rule_source = ? AND status = ?", ruleId, ruleSource, ModelQuotaUsageStatusActive).
		Updates(map[string]any{"status": ModelQuotaUsageStatusExpired, "updated_at": common.GetTimestamp()}).Error; err != nil {
		return err
	}
	for _, usage := range usages {
		CacheDeleteModelQuotaUsage(usage.Id)
	}
	return nil
}

// BatchUpdateModelQuotaUsage is called by the batch updater to flush accumulated deltas
func BatchUpdateModelQuotaUsage(store map[int]int) {
	for usageId, delta := range store {
		if err := increaseModelQuotaUsageDB(usageId, int64(delta)); err != nil {
			common.SysLog("failed to batch update model quota usage: " + err.Error())
		}
	}
}
