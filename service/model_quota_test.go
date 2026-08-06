package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createGroupAllRule(t *testing.T, group, period string, quotaLimit, tokenLimit int64) model.ModelQuotaGroupRule {
	t.Helper()
	rule := model.ModelQuotaGroupRule{
		GroupName: group, Scope: model.ModelQuotaScopeAll,
		Period: period, QuotaLimit: quotaLimit, TokenLimit: tokenLimit,
		Enabled: true,
	}
	require.NoError(t, model.DB.Create(&rule).Error)
	t.Cleanup(func() {
		model.DB.Where("rule_id = ? AND rule_source = ?", rule.Id, model.ModelQuotaRuleSourceGroup).Delete(&model.UserModelQuotaUsage{})
		model.DB.Delete(&rule)
	})
	return rule
}

func createModelQuotaUsage(t *testing.T, userID, ruleID int, quotaLimit, quotaUsed, tokenLimit, tokenUsed int64) model.UserModelQuotaUsage {
	t.Helper()
	usage := model.UserModelQuotaUsage{
		UserId: userID, RuleId: ruleID, RuleSource: model.ModelQuotaRuleSourceGroup,
		ModelPattern: "*", QuotaLimit: quotaLimit, QuotaUsed: quotaUsed,
		TokenLimit: tokenLimit, TokenUsed: tokenUsed,
		PeriodStart: 1, PeriodEnd: 4_102_444_800,
		Status: model.ModelQuotaUsageStatusActive,
	}
	require.NoError(t, model.DB.Create(&usage).Error)
	return usage
}

func TestCheckPreFundingModelQuotaAllScopeMatchesEveryModel(t *testing.T) {
	setupModelQuotaTestDB(t)
	createGroupAllRule(t, "all-scope-group", model.ModelQuotaPeriodMonthly, 0, 100_000_000)

	result, err := CheckPreFundingModelQuota(9101, "claude-opus", "all-scope-group", 1)
	require.NoError(t, err)
	require.True(t, result.Passed)
	require.NotEmpty(t, result.UsageIDs)
}

func TestCheckPreFundingModelQuotaLeavesPlanRulesToSubscriptionBilling(t *testing.T) {
	setupModelQuotaTestDB(t)
	rule := model.ModelQuotaPlanRule{
		PlanId: 1, Scope: model.ModelQuotaScopeAll,
		QuotaLimit: 1, Enabled: true,
	}
	require.NoError(t, model.DB.Create(&rule).Error)
	t.Cleanup(func() { model.DB.Delete(&rule) })

	result, err := CheckPreFundingModelQuota(9103, "gpt-5", "plan-only-group", 100)
	require.NoError(t, err)
	require.True(t, result.Passed)
	require.Empty(t, result.UsageIDs)
}

func TestCheckPreFundingModelQuotaUsesPracticalTokenLimit(t *testing.T) {
	setupModelQuotaTestDB(t)
	tests := []struct {
		name       string
		tokenUsed  int64
		wantPassed bool
	}{
		{"below limit", 99, true},
		{"at limit", 100, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := fmt.Sprintf("token-limit-group-%d", i)
			rule := createGroupAllRule(t, group, model.ModelQuotaPeriodDaily, 0, 100)
			createModelQuotaUsage(t, 9102+i, rule.Id, 0, 0, 100, tt.tokenUsed)

			result, err := CheckPreFundingModelQuota(9102+i, "gpt-5", group, 1)
			require.NoError(t, err)
			require.Equal(t, tt.wantPassed, result.Passed)
			if !tt.wantPassed {
				require.Equal(t, types.ErrorCodeAllModelsTokenLimitExhausted, result.APIError.GetErrorCode())
				require.Equal(t, http.StatusForbidden, result.APIError.StatusCode)
				require.Contains(t, result.APIError.Error(), "Token 用量已达到上限")
			}
		})
	}
}

func TestCheckPreFundingModelQuotaAmountErrorsAreTyped(t *testing.T) {
	setupModelQuotaTestDB(t)
	tests := []struct {
		name     string
		used     int64
		preQuota int
		wantCode types.ErrorCode
	}{
		{"exhausted", 100, 1, types.ErrorCodeAllModelsAmountLimitExhausted},
		{"insufficient", 90, 11, types.ErrorCodeAllModelsAmountLimitInsufficient},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := "amount-limit-" + tt.name
			rule := createGroupAllRule(t, group, model.ModelQuotaPeriodMonthly, 100, 0)
			createModelQuotaUsage(t, 9200+i, rule.Id, 100, tt.used, 0, 0)

			result, err := CheckPreFundingModelQuota(9200+i, "gpt-5", group, tt.preQuota)
			require.NoError(t, err)
			require.False(t, result.Passed)
			require.Equal(t, tt.wantCode, result.APIError.GetErrorCode())
			require.Equal(t, http.StatusForbidden, result.APIError.StatusCode)
			require.NotEmpty(t, result.APIError.Error())
		})
	}
}

func TestCalculateModelQuotaPeriodBoundsUsesShanghai(t *testing.T) {
	now := time.Date(2026, time.August, 6, 20, 30, 0, 0, time.UTC)
	tests := []struct {
		period    string
		wantStart time.Time
		wantEnd   time.Time
	}{
		{model.ModelQuotaPeriodDaily, time.Date(2026, time.August, 6, 16, 0, 0, 0, time.UTC), time.Date(2026, time.August, 7, 16, 0, 0, 0, time.UTC)},
		{model.ModelQuotaPeriodWeekly, time.Date(2026, time.August, 2, 16, 0, 0, 0, time.UTC), time.Date(2026, time.August, 9, 16, 0, 0, 0, time.UTC)},
		{model.ModelQuotaPeriodMonthly, time.Date(2026, time.July, 31, 16, 0, 0, 0, time.UTC), time.Date(2026, time.August, 31, 16, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		start, end := calculateModelQuotaPeriodBoundsAt(tt.period, now)
		require.Equal(t, tt.wantStart.Unix(), start)
		require.Equal(t, tt.wantEnd.Unix(), end)
	}
}

func TestCheckPreFundingModelQuotaReturnsRuleQueryError(t *testing.T) {
	setupModelQuotaTestDB(t)
	require.NoError(t, model.DB.Migrator().DropTable(&model.ModelQuotaUserRule{}))
	t.Cleanup(func() { require.NoError(t, model.DB.AutoMigrate(&model.ModelQuotaUserRule{})) })

	result, err := CheckPreFundingModelQuota(9301, "gpt-5", "default", 1)
	require.Error(t, err)
	require.Nil(t, result)
}

func TestCheckPreFundingModelQuotaFallsBackWhenRedisFails(t *testing.T) {
	setupModelQuotaTestDB(t)
	rule := createGroupAllRule(t, "redis-fallback-group", model.ModelQuotaPeriodTotal, 100, 100)
	createModelQuotaUsage(t, 9401, rule.Id, 100, 10, 100, 10)

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	require.NoError(t, client.Close())
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})

	result, err := CheckPreFundingModelQuota(9401, "gpt-5", "redis-fallback-group", 1)
	require.NoError(t, err)
	require.True(t, result.Passed)
}

type failModelQuotaCacheRefillHook struct{}

func (failModelQuotaCacheRefillHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (failModelQuotaCacheRefillHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (failModelQuotaCacheRefillHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, errors.New("cache refill failed")
}

func (failModelQuotaCacheRefillHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestCheckPreFundingModelQuotaIgnoresCacheRefillFailure(t *testing.T) {
	setupModelQuotaTestDB(t)
	createGroupAllRule(t, "redis-refill-group", model.ModelQuotaPeriodTotal, 100, 0)

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	client.AddHook(failModelQuotaCacheRefillHook{})
	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		_ = client.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})

	result, err := CheckPreFundingModelQuota(9501, "gpt-5", "redis-refill-group", 1)
	require.NoError(t, err)
	require.True(t, result.Passed)
}

func setupModelQuotaTestDB(t *testing.T) {
	t.Helper()
	// If model.DB is already set up (by another test), just ensure our tables exist
	if model.DB != nil {
		if !model.DB.Migrator().HasTable(&model.ModelQuotaGroupRule{}) {
			require.NoError(t, model.DB.AutoMigrate(
				&model.ModelQuotaGroupRule{},
				&model.ModelQuotaPlanRule{},
				&model.ModelQuotaUserRule{},
				&model.UserModelQuotaUsage{},
			))
		}
		return
	}
	// Create fresh in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.ModelQuotaGroupRule{},
		&model.ModelQuotaPlanRule{},
		&model.ModelQuotaUserRule{},
		&model.UserModelQuotaUsage{},
	))
}

func TestMatchModel_Exact(t *testing.T) {
	setupModelQuotaTestDB(t)
	require.True(t, matchModel("gpt-5.5", "gpt-5.5", model.ModelQuotaMatchModeExact))
	require.False(t, matchModel("gpt-5.5-mini", "gpt-5.5", model.ModelQuotaMatchModeExact))
	require.False(t, matchModel("gpt-5.5-2025-06-30", "gpt-5.5", model.ModelQuotaMatchModeExact))
}

func TestMatchModel_Prefix(t *testing.T) {
	setupModelQuotaTestDB(t)
	require.True(t, matchModel("gpt-5.5", "gpt-5.5", model.ModelQuotaMatchModePrefix))
	require.True(t, matchModel("gpt-5.5-mini", "gpt-5.5", model.ModelQuotaMatchModePrefix))
	require.True(t, matchModel("gpt-5.5-2025-06-30", "gpt-5.5", model.ModelQuotaMatchModePrefix))
	require.False(t, matchModel("gpt-4o", "gpt-5.5", model.ModelQuotaMatchModePrefix))
}

func TestCheckModelQuota_NoRules(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_group_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})
	// No rules → no restriction, pass
	result, err := CheckPreFundingModelQuota(999, "gpt-5.5", "default", 100)
	require.NoError(t, err)
	require.True(t, result.Passed)
	require.Len(t, result.UsageIDs, 0)
}

func TestCheckModelQuota_GroupRulePass(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_group_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})

	// Create group rule: gpt-5.5 limit 1000
	rule := &model.ModelQuotaGroupRule{
		GroupName:    "default",
		ModelPattern: "gpt-5.5",
		MatchMode:    model.ModelQuotaMatchModeExact,
		QuotaLimit:   1000,
		Enabled:      true,
	}
	require.NoError(t, model.DB.Create(rule).Error)

	// Pre-consume 100, should pass
	result, err := CheckPreFundingModelQuota(101, "gpt-5.5", "default", 100)
	require.NoError(t, err)
	require.True(t, result.Passed)
}

func TestCheckModelQuota_GroupRuleExhausted(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_group_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})

	// Create group rule: gpt-5.5 limit 500
	rule := &model.ModelQuotaGroupRule{
		GroupName:    "default",
		ModelPattern: "gpt-5.5",
		MatchMode:    model.ModelQuotaMatchModeExact,
		QuotaLimit:   500,
		Enabled:      true,
	}
	require.NoError(t, model.DB.Create(rule).Error)

	// Create existing usage: already used 450
	usage := &model.UserModelQuotaUsage{
		UserId: 201, RuleId: rule.Id, RuleSource: model.ModelQuotaRuleSourceGroup,
		ModelPattern: "gpt-5.5", QuotaLimit: 500, QuotaUsed: 450,
		PeriodStart: common.GetTimestamp() - 1000, PeriodEnd: common.GetTimestamp() + 3600, Status: model.ModelQuotaUsageStatusActive,
	}
	require.NoError(t, model.DB.Create(usage).Error)

	// Pre-consume 100, 450+100=550 > 500, should fail
	result, err := CheckPreFundingModelQuota(201, "gpt-5.5", "default", 100)
	require.NoError(t, err)
	require.False(t, result.Passed)
	require.NotNil(t, result.APIError)
}

func TestCheckModelQuota_PrefixMatch(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_group_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})

	rule := &model.ModelQuotaGroupRule{
		GroupName:    "default",
		ModelPattern: "gpt-5.5",
		MatchMode:    model.ModelQuotaMatchModePrefix,
		QuotaLimit:   1000,
		Enabled:      true,
	}
	require.NoError(t, model.DB.Create(rule).Error)

	// gpt-5.5-mini should match the prefix rule
	result, err := CheckPreFundingModelQuota(301, "gpt-5.5-mini", "default", 100)
	require.NoError(t, err)
	require.True(t, result.Passed)
}

func TestCheckModelQuota_MultipleRules(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_group_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})

	// Rule 1: gpt-5.5 prefix, limit 1000
	rule1 := &model.ModelQuotaGroupRule{
		GroupName: "default", ModelPattern: "gpt-5.5", MatchMode: model.ModelQuotaMatchModePrefix,
		QuotaLimit: 1000, Enabled: true, SortOrder: 0,
	}
	require.NoError(t, model.DB.Create(rule1).Error)

	// Rule 2: gpt-5.5 exact, limit 500
	rule2 := &model.ModelQuotaGroupRule{
		GroupName: "default", ModelPattern: "gpt-5.5", MatchMode: model.ModelQuotaMatchModeExact,
		QuotaLimit: 500, Enabled: true, SortOrder: 1,
	}
	require.NoError(t, model.DB.Create(rule2).Error)

	// Both rules match, both should pass with 100 pre-consume
	result, err := CheckPreFundingModelQuota(401, "gpt-5.5", "default", 100)
	require.NoError(t, err)
	require.True(t, result.Passed)
	require.Len(t, result.UsageIDs, 2)
}

func TestCheckModelQuota_DisabledRuleSkipped(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_group_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})

	rule := &model.ModelQuotaGroupRule{
		GroupName:    "default",
		ModelPattern: "gpt-5.5",
		MatchMode:    model.ModelQuotaMatchModeExact,
		QuotaLimit:   100,
		Enabled:      false,
	}
	require.NoError(t, model.DB.Create(rule).Error)

	// Disabled rule should be skipped → pass
	result, err := CheckPreFundingModelQuota(501, "gpt-5.5", "default", 100)
	require.NoError(t, err)
	require.True(t, result.Passed)
}

func TestCheckModelQuota_PeriodExpiredCreatesFreshUsage(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_group_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})

	rule := &model.ModelQuotaGroupRule{
		GroupName: "default", ModelPattern: "gpt-5.5", MatchMode: model.ModelQuotaMatchModeExact,
		Period: model.ModelQuotaPeriodDaily, QuotaLimit: 500, Enabled: true,
	}
	require.NoError(t, model.DB.Create(rule).Error)

	oldUsage := &model.UserModelQuotaUsage{
		UserId: 701, RuleId: rule.Id, RuleSource: model.ModelQuotaRuleSourceGroup,
		ModelPattern: "gpt-5.5", QuotaLimit: 500, QuotaUsed: 499,
		PeriodStart: common.GetTimestamp() - 86400*2, PeriodEnd: common.GetTimestamp() - 3600,
		Status: model.ModelQuotaUsageStatusActive,
	}
	require.NoError(t, model.DB.Create(oldUsage).Error)

	result, err := CheckPreFundingModelQuota(701, "gpt-5.5", "default", 100)
	require.NoError(t, err)
	require.True(t, result.Passed)
	require.Len(t, result.UsageIDs, 1)
	require.NotEqual(t, oldUsage.Id, result.UsageIDs[0])

	var refreshed model.UserModelQuotaUsage
	require.NoError(t, model.DB.First(&refreshed, result.UsageIDs[0]).Error)
	require.Equal(t, int64(0), refreshed.QuotaUsed)
	require.Greater(t, refreshed.PeriodEnd, common.GetTimestamp())
}

func TestCheckModelQuota_ExpiredUsageIgnored(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_group_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})

	rule := &model.ModelQuotaGroupRule{
		GroupName: "default", ModelPattern: "gpt-5.5", MatchMode: model.ModelQuotaMatchModeExact,
		QuotaLimit: 500, Enabled: true,
	}
	require.NoError(t, model.DB.Create(rule).Error)

	// Expired usage (period_end < now)
	expiredUsage := &model.UserModelQuotaUsage{
		UserId: 601, RuleId: rule.Id, RuleSource: model.ModelQuotaRuleSourceGroup,
		ModelPattern: "gpt-5.5", QuotaLimit: 500, QuotaUsed: 499,
		PeriodStart: 500, PeriodEnd: 600, Status: model.ModelQuotaUsageStatusExpired,
	}
	require.NoError(t, model.DB.Create(expiredUsage).Error)

	// New check with fresh period → should pass (expired usage ignored)
	result, err := CheckPreFundingModelQuota(601, "gpt-5.5", "default", 100)
	require.NoError(t, err)
	require.True(t, result.Passed)
}

func TestRecordModelQuotaUsageStoresActualAmountAndTokens(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() { model.DB.Exec("DELETE FROM user_model_quota_usage") })
	usage := model.UserModelQuotaUsage{
		UserId: 9901, RuleId: 9902, RuleSource: model.ModelQuotaRuleSourceUser,
		ModelPattern: "*", QuotaLimit: 10_000, TokenLimit: 100_000,
		PeriodStart: 1, PeriodEnd: 4_102_444_800, Status: model.ModelQuotaUsageStatusActive,
	}
	require.NoError(t, model.DB.Create(&usage).Error)

	require.NoError(t, RecordModelQuotaUsage([]int{usage.Id}, 321, 12_345))
	require.NoError(t, model.DB.First(&usage, usage.Id).Error)
	require.EqualValues(t, 321, usage.QuotaUsed)
	require.EqualValues(t, 12_345, usage.TokenUsed)
}

func TestRecordModelQuotaFromContextIsIdempotent(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() { model.DB.Exec("DELETE FROM user_model_quota_usage") })
	usage := model.UserModelQuotaUsage{
		UserId: 9903, RuleId: 9904, RuleSource: model.ModelQuotaRuleSourceUser,
		ModelPattern: "*", PeriodStart: 1, PeriodEnd: 4_102_444_800,
		Status: model.ModelQuotaUsageStatusActive,
	}
	require.NoError(t, model.DB.Create(&usage).Error)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set(modelQuotaUsageContextKey, []int{usage.Id})
	require.NoError(t, recordModelQuotaFromContext(ctx, 50, 75))
	require.NoError(t, recordModelQuotaFromContext(ctx, 50, 75))
	require.NoError(t, model.DB.First(&usage, usage.Id).Error)
	require.EqualValues(t, 50, usage.QuotaUsed)
	require.EqualValues(t, 75, usage.TokenUsed)
}

// TestCheckModelQuota_UserRuleBlocksIndependently verifies that a personal
// user rule is enforced even when the group rule would allow the request.
func TestCheckModelQuota_UserRuleBlocksIndependently(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_group_rules")
		model.DB.Exec("DELETE FROM model_quota_user_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})

	// Group rule: generous limit
	groupRule := &model.ModelQuotaGroupRule{
		GroupName: "default", ModelPattern: "gpt-5.5", MatchMode: model.ModelQuotaMatchModeExact,
		Period: model.ModelQuotaPeriodTotal, QuotaLimit: 100000, Enabled: true,
	}
	require.NoError(t, model.DB.Create(groupRule).Error)

	// User rule: tight per-day limit for user 701
	userRule := &model.ModelQuotaUserRule{
		UserId: 701, Username: "alice",
		ModelPattern: "gpt-5.5", MatchMode: model.ModelQuotaMatchModeExact,
		Period: model.ModelQuotaPeriodDaily, QuotaLimit: 500, Enabled: true,
	}
	require.NoError(t, model.DB.Create(userRule).Error)

	// Pre-consume 600 → exceeds user limit (500) even though group allows 100000
	result, err := CheckPreFundingModelQuota(701, "gpt-5.5", "default", 600)
	require.NoError(t, err)
	require.False(t, result.Passed)
	require.Equal(t, types.ErrorCodeModelAmountLimitInsufficient, result.APIError.GetErrorCode())
}

// TestCheckModelQuota_UserRuleDeletedStopsBlocking verifies that deleting a
// rule stops enforcement while preserving its historical usage snapshot.
func TestCheckModelQuota_UserRuleDeletedStopsBlocking(t *testing.T) {
	setupModelQuotaTestDB(t)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM model_quota_user_rules")
		model.DB.Exec("DELETE FROM user_model_quota_usage")
	})

	userRule := &model.ModelQuotaUserRule{
		UserId: 801, Username: "bob",
		ModelPattern: "gpt-5.5", MatchMode: model.ModelQuotaMatchModeExact,
		Period: model.ModelQuotaPeriodDaily, QuotaLimit: 100, Enabled: true,
	}
	require.NoError(t, model.DB.Create(userRule).Error)

	// First request creates usage and consumes some quota
	result1, err := CheckPreFundingModelQuota(801, "gpt-5.5", "default", 50)
	require.NoError(t, err)
	require.True(t, result1.Passed)
	require.Len(t, result1.UsageIDs, 1)

	// Delete only the rule; historical usage remains available for audit.
	require.NoError(t, model.DB.Delete(&model.ModelQuotaUserRule{}, userRule.Id).Error)
	var historicalCount int64
	require.NoError(t, model.DB.Model(&model.UserModelQuotaUsage{}).Where("rule_id = ? AND rule_source = ?", userRule.Id, model.ModelQuotaRuleSourceUser).Count(&historicalCount).Error)
	require.EqualValues(t, 1, historicalCount)

	// After deletion, the user should not be blocked anymore
	result2, err := CheckPreFundingModelQuota(801, "gpt-5.5", "default", 100000)
	require.NoError(t, err)
	require.True(t, result2.Passed)
}
