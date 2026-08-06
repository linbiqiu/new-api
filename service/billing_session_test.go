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
	for _, table := range []string{"subscription_pre_consume_records", "user_subscriptions", "subscription_plans", "tokens", "users"} {
		require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
	}
	t.Cleanup(func() {
		for _, table := range []string{"subscription_pre_consume_records", "user_subscriptions", "subscription_plans", "tokens", "users"} {
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
