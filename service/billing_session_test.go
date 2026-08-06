package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func prepareBillingQuotaErrorTest(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(
		&model.ModelQuotaPlanRule{}, &model.UserModelQuotaUsage{},
	))
	for _, table := range []string{"user_model_quota_usage", "model_quota_plan_rules", "subscription_pre_consume_records", "user_subscriptions", "subscription_plans", "tokens", "users"} {
		require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
	}
	t.Cleanup(func() {
		for _, table := range []string{"user_model_quota_usage", "model_quota_plan_rules", "subscription_pre_consume_records", "user_subscriptions", "subscription_plans", "tokens", "users"} {
			model.DB.Exec("DELETE FROM " + table)
		}
	})
}

func newBillingQuotaTestContext() *gin.Context {
	context, _ := gin.CreateTestContext(nil)
	return context
}

func newBillingQuotaRelayInfo(userID, tokenID int, tokenKey, preference string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: tokenKey,
		OriginModelName: "gpt-5", RequestId: "req-billing-quota",
		ForcePreConsume: true,
		UserSetting:     dto.UserSetting{BillingPreference: preference},
	}
}

func TestBillingSessionReturnsChineseQuotaErrors(t *testing.T) {
	tests := []struct {
		name       string
		preference string
		userQuota  int
		tokenQuota int
		seedSub    func(t *testing.T, userID int)
		wantCode   types.ErrorCode
		wantText   string
	}{
		{"wallet exhausted", "wallet_only", 0, 100, nil, types.ErrorCodeWalletQuotaExhausted, "当前账户余额已使用完毕"},
		{"wallet insufficient", "wallet_only", 10, 100, nil, types.ErrorCodeWalletQuotaInsufficient, "当前账户余额不足以完成本次请求"},
		{"subscription unavailable", "subscription_only", 100, 100, nil, types.ErrorCodeSubscriptionUnavailable, "当前没有可用的订阅计划"},
		{"subscription expired", "subscription_only", 100, 100, func(t *testing.T, userID int) {
			now := time.Now().Unix()
			plan := &model.SubscriptionPlan{Title: "已过期套餐", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100, Enabled: true}
			require.NoError(t, model.DB.Create(plan).Error)
			model.InvalidateSubscriptionPlanCache(plan.Id)
			require.NoError(t, model.DB.Create(&model.UserSubscription{UserId: userID, PlanId: plan.Id, AmountTotal: 100, AmountUsed: 50, StartTime: now - 7200, EndTime: now - 60, Status: "active"}).Error)
		}, types.ErrorCodeSubscriptionExpired, "您的“已过期套餐”已到期"},
		{"subscription insufficient", "subscription_only", 100, 100, func(t *testing.T, userID int) {
			now := time.Now().Unix()
			plan := &model.SubscriptionPlan{Title: "专业版", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100, Enabled: true}
			require.NoError(t, model.DB.Create(plan).Error)
			model.InvalidateSubscriptionPlanCache(plan.Id)
			require.NoError(t, model.DB.Create(&model.UserSubscription{UserId: userID, PlanId: plan.Id, AmountTotal: 100, AmountUsed: 90, StartTime: now - 60, EndTime: now + 3600, Status: "active"}).Error)
		}, types.ErrorCodeSubscriptionPeriodInsufficient, "本周期剩余额度不足以完成本次请求"},
		{"API token exhausted", "wallet_only", 100, 0, nil, types.ErrorCodeAPITokenQuotaExhausted, "当前 API 令牌额度已使用完毕"},
		{"API token insufficient", "wallet_only", 100, 10, nil, types.ErrorCodeAPITokenQuotaInsufficient, "当前 API 令牌剩余额度不足以完成本次请求"},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepareBillingQuotaErrorTest(t)
			userID := 10_000 + index
			tokenID := 20_000 + index
			tokenKey := "billing-quota-token-" + tt.name
			require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: "billing-user-" + tt.name, Quota: tt.userQuota, Status: common.UserStatusEnabled}).Error)
			require.NoError(t, model.DB.Create(&model.Token{Id: tokenID, UserId: userID, Key: tokenKey, Name: "billing", Status: common.TokenStatusEnabled, RemainQuota: tt.tokenQuota}).Error)
			if tt.seedSub != nil {
				tt.seedSub(t, userID)
			}

			_, apiErr := NewBillingSession(newBillingQuotaTestContext(), newBillingQuotaRelayInfo(userID, tokenID, tokenKey, tt.preference), 20)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
			require.Equal(t, tt.wantCode, apiErr.GetErrorCode())
			require.Contains(t, apiErr.Error(), tt.wantText)
			require.NotContains(t, apiErr.Error(), "quota")
			require.NotContains(t, apiErr.Error(), "need=")
			require.NotContains(t, apiErr.Error(), "subscription quota insufficient")
		})
	}
}

func TestBillingSessionStrictSubscriptionDoesNotExposeFallbackPolicy(t *testing.T) {
	prepareBillingQuotaErrorTest(t)

	const userID = 30_001
	const tokenID = 30_002
	const tokenKey = "strict-subscription-token"
	now := time.Now().Unix()
	plan := &model.SubscriptionPlan{Title: "严格套餐", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100, Enabled: true}
	require.NoError(t, model.DB.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		UserId: userID, PlanId: plan.Id, AmountTotal: 100, AmountUsed: 90,
		StartTime: now - 60, EndTime: now + 3600, Status: "active", AllowWalletOverflow: false,
	}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: "strict-user", Quota: 1000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: tokenID, UserId: userID, Key: tokenKey, Name: "strict", Status: common.TokenStatusEnabled, RemainQuota: 1000}).Error)

	_, apiErr := NewBillingSession(newBillingQuotaTestContext(), newBillingQuotaRelayInfo(userID, tokenID, tokenKey, "subscription_first"), 20)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeSubscriptionPeriodInsufficient, apiErr.GetErrorCode())
	require.NotContains(t, apiErr.Error(), "钱包")
	require.NotContains(t, apiErr.Error(), "回退")
}

func TestBillingSessionChecksOnlyActualFundedSubscriptionPlan(t *testing.T) {
	prepareBillingQuotaErrorTest(t)
	const userID, tokenID = 31_001, 31_002
	const tokenKey = "actual-funded-subscription-token"
	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: "funded-user", Quota: 1000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: tokenID, UserId: userID, Key: tokenKey, Name: "funded", Status: common.TokenStatusEnabled, RemainQuota: 1000}).Error)

	firstPlan := model.SubscriptionPlan{Title: "额度不足套餐", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100, Enabled: true}
	secondPlan := model.SubscriptionPlan{Title: "实际出资套餐", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, model.DB.Create(&firstPlan).Error)
	require.NoError(t, model.DB.Create(&secondPlan).Error)
	model.InvalidateSubscriptionPlanCache(firstPlan.Id)
	model.InvalidateSubscriptionPlanCache(secondPlan.Id)
	firstSub := model.UserSubscription{UserId: userID, PlanId: firstPlan.Id, AmountTotal: 100, AmountUsed: 95, StartTime: now - 3600, EndTime: now + 1800, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 1800}
	secondSub := model.UserSubscription{UserId: userID, PlanId: secondPlan.Id, AmountTotal: 1000, AmountUsed: 0, StartTime: now - 3600, EndTime: now + 3600, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 3600}
	require.NoError(t, model.DB.Create(&firstSub).Error)
	require.NoError(t, model.DB.Create(&secondSub).Error)
	firstRule := model.ModelQuotaPlanRule{PlanId: firstPlan.Id, Scope: model.ModelQuotaScopeAll, QuotaLimit: 1, Enabled: true}
	secondRule := model.ModelQuotaPlanRule{PlanId: secondPlan.Id, Scope: model.ModelQuotaScopeAll, QuotaLimit: 1000, Enabled: true}
	require.NoError(t, model.DB.Create(&firstRule).Error)
	require.NoError(t, model.DB.Create(&secondRule).Error)

	ctx := newBillingQuotaTestContext()
	session, apiErr := NewBillingSession(ctx, newBillingQuotaRelayInfo(userID, tokenID, tokenKey, "subscription_only"), 20)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, secondSub.Id, session.relayInfo.SubscriptionId)
	rawUsageIDs, exists := ctx.Get(modelQuotaUsageContextKey)
	require.True(t, exists)
	usageIDs, ok := rawUsageIDs.([]int)
	require.True(t, ok)
	require.Len(t, usageIDs, 1)
	var usage model.UserModelQuotaUsage
	require.NoError(t, model.DB.First(&usage, usageIDs[0]).Error)
	require.Equal(t, secondRule.Id, usage.RuleId)
	require.Equal(t, secondSub.Id, usage.SubscriptionId)
}

func TestBillingSessionRollsBackWhenFundedPlanLimitRejects(t *testing.T) {
	prepareBillingQuotaErrorTest(t)
	const userID, tokenID = 32_001, 32_002
	const tokenKey = "plan-limit-rollback-token"
	now := time.Now().Unix()
	plan := model.SubscriptionPlan{Title: "受限套餐", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	subscription := model.UserSubscription{UserId: userID, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 100, StartTime: now - 3600, EndTime: now + 3600, Status: "active"}
	require.NoError(t, model.DB.Create(&subscription).Error)
	require.NoError(t, model.DB.Create(&model.ModelQuotaPlanRule{PlanId: plan.Id, Scope: model.ModelQuotaScopeAll, QuotaLimit: 10, Enabled: true}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: "rollback-user", Quota: 1000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: tokenID, UserId: userID, Key: tokenKey, Name: "rollback", Status: common.TokenStatusEnabled, RemainQuota: 1000}).Error)

	_, apiErr := NewBillingSession(newBillingQuotaTestContext(), newBillingQuotaRelayInfo(userID, tokenID, tokenKey, "subscription_only"), 20)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeAllModelsAmountLimitInsufficient, apiErr.GetErrorCode())
	require.NoError(t, model.DB.First(&subscription, subscription.Id).Error)
	require.EqualValues(t, 100, subscription.AmountUsed)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	require.Equal(t, 1000, token.RemainQuota)
}

func TestBillingSessionWalletFallbackDoesNotApplyPlanRules(t *testing.T) {
	prepareBillingQuotaErrorTest(t)
	const userID, tokenID = 33_001, 33_002
	const tokenKey = "wallet-fallback-plan-token"
	now := time.Now().Unix()
	plan := model.SubscriptionPlan{Title: "可回退套餐", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100, Enabled: true}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		UserId: userID, PlanId: plan.Id, AmountTotal: 100, AmountUsed: 100,
		StartTime: now - 3600, EndTime: now + 3600, Status: "active", AllowWalletOverflow: true,
	}).Error)
	require.NoError(t, model.DB.Create(&model.ModelQuotaPlanRule{PlanId: plan.Id, Scope: model.ModelQuotaScopeAll, QuotaLimit: 1, Enabled: true}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: "wallet-fallback-user", Quota: 1000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: tokenID, UserId: userID, Key: tokenKey, Name: "wallet-fallback", Status: common.TokenStatusEnabled, RemainQuota: 1000}).Error)

	ctx := newBillingQuotaTestContext()
	session, apiErr := NewBillingSession(ctx, newBillingQuotaRelayInfo(userID, tokenID, tokenKey, "subscription_first"), 20)
	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceWallet, session.funding.Source())
	_, exists := ctx.Get(modelQuotaUsageContextKey)
	require.False(t, exists)
}
