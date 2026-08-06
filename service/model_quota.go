package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"gorm.io/gorm"
)

// ModelQuotaCheckResult holds the result of a model quota check
type ModelQuotaCheckResult struct {
	Passed   bool
	UsageIDs []int
	APIError *types.NewAPIError
}

const modelQuotaUsageContextKey = "model_quota_usage_ids"

// matchModel checks if modelName matches the pattern based on mode
func matchModel(modelName, pattern, mode string) bool {
	switch mode {
	case model.ModelQuotaMatchModeExact:
		return modelName == pattern
	case model.ModelQuotaMatchModePrefix:
		return strings.HasPrefix(modelName, pattern)
	}
	return false
}

// matchedRule is an internal struct that unifies group and plan rules
type matchedRule struct {
	RuleId       int
	RuleSource   string
	Scope        string
	ModelPattern string
	MatchMode    string
	QuotaLimit   int64
	TokenLimit   int64
	Period       string
}

var modelQuotaShanghaiLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(fmt.Sprintf("load model quota timezone: %v", err))
	}
	return location
}()

func calculateModelQuotaPeriodBoundsAt(period string, now time.Time) (int64, int64) {
	now = now.In(modelQuotaShanghaiLocation)

	switch period {
	case model.ModelQuotaPeriodDaily:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, modelQuotaShanghaiLocation)
		return start.Unix(), start.AddDate(0, 0, 1).Unix()

	case model.ModelQuotaPeriodWeekly:
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start := time.Date(now.Year(), now.Month(), now.Day()-weekday+1, 0, 0, 0, 0, modelQuotaShanghaiLocation)
		return start.Unix(), start.AddDate(0, 0, 7).Unix()

	case model.ModelQuotaPeriodMonthly:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, modelQuotaShanghaiLocation)
		return start.Unix(), start.AddDate(0, 1, 0).Unix()

	case model.ModelQuotaPeriodTotal:
		return 0, time.Date(2099, 1, 1, 0, 0, 0, 0, modelQuotaShanghaiLocation).Unix()

	default:
		return 0, 0
	}
}

// FindMatchingModelQuotaRules finds all rules that match the given model for the user.
// Matching is always driven by the CURRENT enabled rule definitions, never by
// historical usage snapshots. This ensures that deleting/disabling a rule or
// changing its limit takes effect immediately.
//
// User and group rules are both returned and enforced with intersection semantics.
// Plan rules are bound to billing subscriptions separately.
func FindMatchingModelQuotaRules(userId int, modelName string, userGroup string) ([]*matchedRule, error) {
	var matched []*matchedRule

	userRules, err := model.GetModelQuotaUserRulesByUserId(userId)
	if err != nil {
		return nil, fmt.Errorf("query user model quota rules: %w", err)
	}
	for _, r := range userRules {
		if r.Scope == model.ModelQuotaScopeAll || matchModel(modelName, r.ModelPattern, r.MatchMode) {
			matched = append(matched, &matchedRule{
				RuleId: r.Id, RuleSource: model.ModelQuotaRuleSourceUser,
				Scope: r.Scope, ModelPattern: r.ModelPattern, MatchMode: r.MatchMode,
				QuotaLimit: r.QuotaLimit, TokenLimit: r.TokenLimit, Period: r.Period,
			})
		}
	}

	groupRules, err := model.GetModelQuotaGroupRulesByGroup(userGroup)
	if err != nil {
		return nil, fmt.Errorf("query group model quota rules: %w", err)
	}
	for _, r := range groupRules {
		if r.Scope == model.ModelQuotaScopeAll || matchModel(modelName, r.ModelPattern, r.MatchMode) {
			matched = append(matched, &matchedRule{
				RuleId: r.Id, RuleSource: model.ModelQuotaRuleSourceGroup,
				Scope: r.Scope, ModelPattern: r.ModelPattern, MatchMode: r.MatchMode,
				QuotaLimit: r.QuotaLimit, TokenLimit: r.TokenLimit, Period: r.Period,
			})
		}
	}

	return matched, nil
}

// CheckPreFundingModelQuota checks user and group limits before billing reserves quota.
func CheckPreFundingModelQuota(
	userId int,
	modelName string,
	userGroup string,
	preQuota int,
) (*ModelQuotaCheckResult, error) {
	rules, err := FindMatchingModelQuotaRules(userId, modelName, userGroup)
	if err != nil {
		return nil, err
	}

	if len(rules) == 0 {
		return &ModelQuotaCheckResult{Passed: true}, nil
	}

	result := &ModelQuotaCheckResult{Passed: true}

	for _, rule := range rules {
		periodStart, periodEnd := calculateModelQuotaPeriodBoundsAt(rule.Period, time.Now())
		if periodEnd == 0 {
			return nil, fmt.Errorf("unsupported model quota period %q for rule %d", rule.Period, rule.RuleId)
		}
		usageID, apiError, err := checkModelQuotaRuleUsage(userId, modelName, preQuota, rule, 0, periodStart, periodEnd)
		if err != nil {
			return nil, err
		}
		if apiError != nil {
			return &ModelQuotaCheckResult{Passed: false, APIError: apiError}, nil
		}
		result.UsageIDs = append(result.UsageIDs, usageID)
	}

	return result, nil
}

// CheckFundedSubscriptionModelQuota applies plan rules only after billing has
// selected and reserved the concrete subscription instance.
func CheckFundedSubscriptionModelQuota(relayInfoModel string, userId int, funding *SubscriptionFunding) (*ModelQuotaCheckResult, error) {
	rules, err := model.GetModelQuotaPlanRulesByPlanId(funding.PlanId)
	if err != nil {
		return nil, fmt.Errorf("query plan model quota rules: %w", err)
	}
	result := &ModelQuotaCheckResult{Passed: true}
	periodStart, periodEnd := funding.PeriodStart, funding.PeriodEnd
	if periodEnd <= 0 {
		periodEnd = time.Date(2099, 1, 1, 0, 0, 0, 0, modelQuotaShanghaiLocation).Unix()
	}
	for _, planRule := range rules {
		if planRule.Scope != model.ModelQuotaScopeAll && !matchModel(relayInfoModel, planRule.ModelPattern, planRule.MatchMode) {
			continue
		}
		rule := &matchedRule{
			RuleId: planRule.Id, RuleSource: model.ModelQuotaRuleSourcePlan,
			Scope: planRule.Scope, ModelPattern: planRule.ModelPattern, MatchMode: planRule.MatchMode,
			QuotaLimit: planRule.QuotaLimit, TokenLimit: planRule.TokenLimit, Period: "subscription",
		}
		usageID, apiError, checkErr := checkModelQuotaRuleUsage(
			userId, relayInfoModel, int(funding.preConsumed), rule, funding.subscriptionId, periodStart, periodEnd,
		)
		if checkErr != nil {
			return nil, checkErr
		}
		if apiError != nil {
			return &ModelQuotaCheckResult{Passed: false, APIError: apiError}, nil
		}
		result.UsageIDs = append(result.UsageIDs, usageID)
	}
	return result, nil
}

func appendModelQuotaUsageIDs(c interface {
	Get(string) (any, bool)
	Set(string, any)
}, usageIDs []int) {
	if len(usageIDs) == 0 {
		return
	}
	existingValue, _ := c.Get(modelQuotaUsageContextKey)
	existing, _ := existingValue.([]int)
	seen := make(map[int]struct{}, len(existing)+len(usageIDs))
	merged := make([]int, 0, len(existing)+len(usageIDs))
	for _, id := range append(existing, usageIDs...) {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, id)
	}
	c.Set(modelQuotaUsageContextKey, merged)
}

func checkModelQuotaRuleUsage(userId int, modelName string, preQuota int, rule *matchedRule, subscriptionId int, periodStart, periodEnd int64) (int, *types.NewAPIError, error) {
	usage, err := getOrCreateModelQuotaUsage(userId, rule, subscriptionId, periodStart, periodEnd)
	if err != nil {
		return 0, nil, fmt.Errorf("get or create model quota usage for user %d rule %d: %w", userId, rule.RuleId, err)
	}
	snapshot := model.ModelQuotaUsageSnapshot{
		QuotaUsed: usage.QuotaUsed, QuotaLimit: usage.QuotaLimit,
		TokenUsed: usage.TokenUsed, TokenLimit: usage.TokenLimit,
	}
	if common.RedisEnabled {
		cached, ok, cacheErr := model.CacheGetModelQuotaUsage(usage.Id)
		if cacheErr != nil {
			RecordUsageGovernanceRedisFallback()
			common.SysError(fmt.Sprintf("model quota cache fallback for usage %d: %v", usage.Id, cacheErr))
		} else if ok {
			snapshot = cached
		} else {
			_ = model.CacheSetModelQuotaUsage(usage.Id, snapshot, usage.PeriodEnd)
		}
	}

	periodLabel := map[string]string{
		model.ModelQuotaPeriodDaily: "每日", model.ModelQuotaPeriodWeekly: "每周",
		model.ModelQuotaPeriodMonthly: "每月", model.ModelQuotaPeriodTotal: "永久累计",
		"subscription": "本周期",
	}[rule.Period]
	permanent := rule.Period == model.ModelQuotaPeriodTotal
	resetAt := time.Unix(periodEnd, 0).In(modelQuotaShanghaiLocation).Format("2006-01-02 15:04")
	if snapshot.QuotaLimit > 0 && snapshot.QuotaUsed >= snapshot.QuotaLimit {
		return 0, newAmountLimitError(amountLimitErrorInput{
			Scope: rule.Scope, PeriodLabel: periodLabel, Model: modelName,
			Limit: snapshot.QuotaLimit, Permanent: permanent, ResetAt: resetAt,
		}), nil
	}
	requested := int64(preQuota)
	if requested < 0 {
		requested = 0
	}
	if snapshot.QuotaLimit > 0 && requested > snapshot.QuotaLimit-snapshot.QuotaUsed {
		return 0, newAmountLimitError(amountLimitErrorInput{
			Scope: rule.Scope, PeriodLabel: periodLabel, Model: modelName,
			Limit: snapshot.QuotaLimit, Remaining: snapshot.QuotaLimit - snapshot.QuotaUsed,
			Required: requested, Permanent: permanent, ResetAt: resetAt,
		}), nil
	}
	if rule.Scope == model.ModelQuotaScopeAll && snapshot.TokenLimit > 0 && snapshot.TokenUsed >= snapshot.TokenLimit {
		return 0, newTokenLimitError(tokenLimitErrorInput{
			PeriodLabel: periodLabel, Limit: snapshot.TokenLimit, Permanent: permanent, ResetAt: resetAt,
		}), nil
	}
	return usage.Id, nil, nil
}

// getOrCreateModelQuotaUsage finds an existing active, non-expired usage record,
// or creates a new one. If the old usage's period has ended, it marks it as expired
// and creates a fresh one with quota_used=0.
func getOrCreateModelQuotaUsage(userId int, rule *matchedRule, subscriptionId int, periodStart int64, periodEnd int64) (*model.UserModelQuotaUsage, error) {
	var usage model.UserModelQuotaUsage
	err := model.DB.Where(
		"user_id = ? AND rule_id = ? AND rule_source = ? AND subscription_id = ? AND period_start = ? AND period_end = ? AND status = ?",
		userId, rule.RuleId, rule.RuleSource, subscriptionId, periodStart, periodEnd, model.ModelQuotaUsageStatusActive,
	).First(&usage).Error
	if err == nil {
		return &usage, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	err = model.DB.Where(
		"user_id = ? AND rule_id = ? AND rule_source = ? AND subscription_id = ? AND status = ? AND period_end > ?",
		userId, rule.RuleId, rule.RuleSource, subscriptionId, model.ModelQuotaUsageStatusActive, common.GetTimestamp(),
	).First(&usage).Error
	if err == nil {
		return &usage, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if err := model.ExpireOutdatedUserModelQuotaUsage(userId, rule.RuleId, rule.RuleSource); err != nil {
		return nil, err
	}

	// Create new usage record with fresh quota
	newUsage := &model.UserModelQuotaUsage{
		UserId:         userId,
		RuleId:         rule.RuleId,
		RuleSource:     rule.RuleSource,
		ModelPattern:   rule.ModelPattern,
		SubscriptionId: subscriptionId,
		QuotaLimit:     rule.QuotaLimit,
		QuotaUsed:      0,
		TokenLimit:     rule.TokenLimit,
		TokenUsed:      0,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		Status:         model.ModelQuotaUsageStatusActive,
	}
	if err := model.DB.Create(newUsage).Error; err != nil {
		var existing model.UserModelQuotaUsage
		lookupErr := model.DB.Where(
			"user_id = ? AND rule_source = ? AND rule_id = ? AND subscription_id = ? AND period_start = ? AND period_end = ?",
			userId, rule.RuleSource, rule.RuleId, subscriptionId, periodStart, periodEnd,
		).First(&existing).Error
		if lookupErr == nil {
			return &existing, nil
		}
		return nil, err
	}

	_ = model.CacheSetModelQuotaUsage(newUsage.Id, model.ModelQuotaUsageSnapshot{
		QuotaLimit: newUsage.QuotaLimit,
		TokenLimit: newUsage.TokenLimit,
	}, newUsage.PeriodEnd)

	return newUsage, nil
}

// RecordModelQuotaUsage updates the usage counters after a request completes
func RecordModelQuotaUsage(usageIds []int, actualQuota, actualTokens int64) error {
	var recordErrors []error
	for _, id := range usageIds {
		if err := model.IncreaseUserModelQuotaUsage(id, actualQuota, actualTokens); err != nil {
			recordErrors = append(recordErrors, fmt.Errorf("record model quota usage %d: %w", id, err))
		}
	}
	return errors.Join(recordErrors...)
}
