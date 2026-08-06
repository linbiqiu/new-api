package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// --- ModelQuotaGroupRule tests ---

func TestCreateModelQuotaGroupRule(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ModelQuotaGroupRule{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM model_quota_group_rules")
	})

	rule := &ModelQuotaGroupRule{
		GroupName:    "default",
		ModelPattern: "gpt-5.5",
		MatchMode:    ModelQuotaMatchModeExact,
		QuotaLimit:   500000,
		Enabled:      true,
		SortOrder:    0,
	}
	require.NoError(t, DB.Create(rule).Error)
	require.NotZero(t, rule.Id)

	var fetched ModelQuotaGroupRule
	require.NoError(t, DB.First(&fetched, rule.Id).Error)
	require.Equal(t, "default", fetched.GroupName)
	require.Equal(t, "gpt-5.5", fetched.ModelPattern)
	require.Equal(t, ModelQuotaMatchModeExact, fetched.MatchMode)
	require.Equal(t, ModelQuotaScopeModel, fetched.Scope)
	require.EqualValues(t, 500000, fetched.QuotaLimit)
	require.True(t, fetched.Enabled)
}

func TestGetModelQuotaGroupRulesByGroup(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ModelQuotaGroupRule{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM model_quota_group_rules")
	})

	require.NoError(t, DB.Create(&ModelQuotaGroupRule{GroupName: "default", ModelPattern: "gpt-5.5", MatchMode: ModelQuotaMatchModeExact, QuotaLimit: 100, Enabled: true, SortOrder: 1}).Error)
	require.NoError(t, DB.Create(&ModelQuotaGroupRule{GroupName: "default", ModelPattern: "claude-opus", MatchMode: ModelQuotaMatchModePrefix, QuotaLimit: 200, Enabled: true, SortOrder: 0}).Error)
	require.NoError(t, DB.Create(&ModelQuotaGroupRule{GroupName: "vip", ModelPattern: "gpt-5.5", MatchMode: ModelQuotaMatchModeExact, QuotaLimit: 999, Enabled: true}).Error)
	require.NoError(t, DB.Create(&ModelQuotaGroupRule{GroupName: "default", ModelPattern: "disabled-model", MatchMode: ModelQuotaMatchModeExact, QuotaLimit: 50, Enabled: false}).Error)

	rules, err := GetModelQuotaGroupRulesByGroup("default")
	require.NoError(t, err)
	require.Len(t, rules, 2, "should only return enabled rules for 'default' group")
	// sort_order ascending: claude-opus(0) before gpt-5.5(1)
	require.Equal(t, "claude-opus", rules[0].ModelPattern)
	require.Equal(t, "gpt-5.5", rules[1].ModelPattern)
}

// --- ModelQuotaPlanRule tests ---

func TestCreateModelQuotaPlanRule(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ModelQuotaPlanRule{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM model_quota_plan_rules")
	})

	rule := &ModelQuotaPlanRule{
		PlanId:       1,
		ModelPattern: "gpt-5.5",
		MatchMode:    ModelQuotaMatchModePrefix,
		QuotaLimit:   1000000,
		Enabled:      true,
	}
	require.NoError(t, DB.Create(rule).Error)
	require.NotZero(t, rule.Id)

	rules, err := GetModelQuotaPlanRulesByPlanId(1)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "gpt-5.5", rules[0].ModelPattern)
	require.EqualValues(t, 1000000, rules[0].QuotaLimit)
}

func TestGetModelQuotaPlanRules_NoResults(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ModelQuotaPlanRule{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM model_quota_plan_rules")
	})

	rules, err := GetModelQuotaPlanRulesByPlanId(999)
	require.NoError(t, err)
	require.Len(t, rules, 0)
}

// --- UserModelQuotaUsage tests ---

func TestCreateUserModelQuotaUsage(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&UserModelQuotaUsage{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_model_quota_usage")
	})

	usage := &UserModelQuotaUsage{
		UserId:         101,
		RuleId:         1,
		RuleSource:     ModelQuotaRuleSourceGroup,
		ModelPattern:   "gpt-5.5",
		SubscriptionId: 0,
		QuotaLimit:     500000,
		QuotaUsed:      0,
		PeriodStart:    1000,
		PeriodEnd:      2000,
		Status:         ModelQuotaUsageStatusActive,
	}
	require.NoError(t, DB.Create(usage).Error)
	require.NotZero(t, usage.Id)
}

func TestGetActiveUserModelQuotaUsage(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&UserModelQuotaUsage{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_model_quota_usage")
	})

	// Active usage for user 101
	require.NoError(t, DB.Create(&UserModelQuotaUsage{UserId: 101, RuleId: 1, RuleSource: ModelQuotaRuleSourceGroup, ModelPattern: "gpt-5.5", QuotaLimit: 500, QuotaUsed: 100, PeriodStart: common.GetTimestamp() - 1000, PeriodEnd: common.GetTimestamp() + 3600, Status: ModelQuotaUsageStatusActive}).Error)
	// Expired usage for user 101
	require.NoError(t, DB.Create(&UserModelQuotaUsage{UserId: 101, RuleId: 2, RuleSource: ModelQuotaRuleSourceGroup, ModelPattern: "claude-opus", QuotaLimit: 300, QuotaUsed: 50, PeriodStart: 500, PeriodEnd: 600, Status: ModelQuotaUsageStatusExpired}).Error)
	// Active usage for user 102
	require.NoError(t, DB.Create(&UserModelQuotaUsage{UserId: 102, RuleId: 1, RuleSource: ModelQuotaRuleSourceGroup, ModelPattern: "gpt-5.5", QuotaLimit: 500, QuotaUsed: 0, PeriodStart: common.GetTimestamp() - 1000, PeriodEnd: common.GetTimestamp() + 3600, Status: ModelQuotaUsageStatusActive}).Error)

	usages, err := GetActiveUserModelQuotaUsage(101)
	require.NoError(t, err)
	require.Len(t, usages, 1, "should only return active usages for user 101")
	require.Equal(t, "gpt-5.5", usages[0].ModelPattern)
	require.EqualValues(t, 100, usages[0].QuotaUsed)
}

func TestIncreaseUserModelQuotaUsage(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&UserModelQuotaUsage{}))
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_model_quota_usage")
	})

	usage := &UserModelQuotaUsage{
		UserId: 201, RuleId: 1, RuleSource: ModelQuotaRuleSourceGroup,
		ModelPattern: "gpt-5.5", QuotaLimit: 1000, QuotaUsed: 100,
		PeriodStart: 1000, PeriodEnd: 99999, Status: ModelQuotaUsageStatusActive,
	}
	require.NoError(t, DB.Create(usage).Error)

	require.NoError(t, IncreaseUserModelQuotaUsage(usage.Id, 50, 25))

	var updated UserModelQuotaUsage
	require.NoError(t, DB.First(&updated, usage.Id).Error)
	require.EqualValues(t, 150, updated.QuotaUsed)
	require.EqualValues(t, 25, updated.TokenUsed)
}

func TestIncreaseUserModelQuotaUsage_Negative(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&UserModelQuotaUsage{}))
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_model_quota_usage")
	})

	usage := &UserModelQuotaUsage{
		UserId: 202, RuleId: 1, RuleSource: ModelQuotaRuleSourceGroup,
		ModelPattern: "gpt-5.5", QuotaLimit: 1000, QuotaUsed: 200,
		PeriodStart: 1000, PeriodEnd: 99999, Status: ModelQuotaUsageStatusActive,
	}
	require.NoError(t, DB.Create(usage).Error)

	require.ErrorContains(t, IncreaseUserModelQuotaUsage(usage.Id, -100, 0), "delta must be non-negative")
	require.ErrorContains(t, IncreaseUserModelQuotaUsage(usage.Id, 0, -100), "delta must be non-negative")
}

func TestResetUserModelQuotaUsage(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&UserModelQuotaUsage{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_model_quota_usage")
	})

	usage := &UserModelQuotaUsage{
		UserId: 301, RuleId: 1, RuleSource: ModelQuotaRuleSourceGroup,
		ModelPattern: "gpt-5.5", QuotaLimit: 1000, QuotaUsed: 800, TokenLimit: 2000, TokenUsed: 1200,
		PeriodStart: 1000, PeriodEnd: 99999, Status: ModelQuotaUsageStatusActive,
	}
	require.NoError(t, DB.Create(usage).Error)

	require.NoError(t, ResetUserModelQuotaUsage(usage.Id))

	var updated UserModelQuotaUsage
	require.NoError(t, DB.First(&updated, usage.Id).Error)
	require.EqualValues(t, 0, updated.QuotaUsed)
	require.EqualValues(t, 0, updated.TokenUsed)
}

func TestCreateAllScopeRuleStoresTokenLimit(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ModelQuotaGroupRule{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM model_quota_group_rules") })

	rule := &ModelQuotaGroupRule{GroupName: "default", Scope: ModelQuotaScopeAll, TokenLimit: 100_000_000, Enabled: true}
	require.NoError(t, DB.Create(rule).Error)

	var stored ModelQuotaGroupRule
	require.NoError(t, DB.First(&stored, rule.Id).Error)
	require.Equal(t, ModelQuotaScopeAll, stored.Scope)
	require.EqualValues(t, 100_000_000, stored.TokenLimit)
}

func TestInitializeModelQuotaRuleScopesNormalizesHistoricalRows(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ModelQuotaGroupRule{}, &ModelQuotaPlanRule{}, &ModelQuotaUserRule{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM model_quota_group_rules")
		DB.Exec("DELETE FROM model_quota_plan_rules")
		DB.Exec("DELETE FROM model_quota_user_rules")
	})

	groupRule := ModelQuotaGroupRule{GroupName: "default", Enabled: true}
	planRule := ModelQuotaPlanRule{PlanId: 10, Enabled: true}
	userRule := ModelQuotaUserRule{UserId: 20, Enabled: true}
	require.NoError(t, DB.Create(&groupRule).Error)
	require.NoError(t, DB.Create(&planRule).Error)
	require.NoError(t, DB.Create(&userRule).Error)
	require.NoError(t, DB.Model(&groupRule).UpdateColumn("scope", "").Error)
	require.NoError(t, DB.Model(&planRule).UpdateColumn("scope", "").Error)
	require.NoError(t, DB.Model(&userRule).UpdateColumn("scope", "").Error)

	require.NoError(t, initializeModelQuotaRuleScopes())
	for _, rule := range []any{&groupRule, &planRule, &userRule} {
		require.NoError(t, DB.First(rule).Error)
	}
	require.Equal(t, ModelQuotaScopeModel, groupRule.Scope)
	require.Equal(t, ModelQuotaScopeModel, planRule.Scope)
	require.Equal(t, ModelQuotaScopeModel, userRule.Scope)
}

func TestUserModelQuotaUsageUniqueIdentity(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&UserModelQuotaUsage{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM user_model_quota_usage") })

	usage := UserModelQuotaUsage{
		UserId: 501, RuleId: 9, RuleSource: ModelQuotaRuleSourcePlan,
		SubscriptionId: 11, ModelPattern: "*", PeriodStart: 100, PeriodEnd: 200,
		Status: ModelQuotaUsageStatusActive,
	}
	require.NoError(t, DB.Create(&usage).Error)
	usage.Id = 0
	require.Error(t, DB.Create(&usage).Error)
}

func TestExpireUserModelQuotaUsage(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&UserModelQuotaUsage{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_model_quota_usage")
	})

	usage := &UserModelQuotaUsage{
		UserId: 401, RuleId: 1, RuleSource: ModelQuotaRuleSourceGroup,
		ModelPattern: "gpt-5.5", QuotaLimit: 1000, QuotaUsed: 500,
		PeriodStart: 1000, PeriodEnd: 99999, Status: ModelQuotaUsageStatusActive,
	}
	require.NoError(t, DB.Create(usage).Error)

	require.NoError(t, ExpireUserModelQuotaUsage(usage.Id))

	var updated UserModelQuotaUsage
	require.NoError(t, DB.First(&updated, usage.Id).Error)
	require.Equal(t, ModelQuotaUsageStatusExpired, updated.Status)
}
