package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestDailyUsageNotificationCardContainsRMBAndSeparateSubscriptions(t *testing.T) {
	notification := buildDailyTokenMilestoneNotification(dailyTokenMilestonePayload{
		UsageDate: "2026-08-06", MilestoneM: 300, TokenUsed: 310_000_000,
		QuotaUsed: int64(common.QuotaPerUnit),
		Subscriptions: []subscriptionUsageDigest{
			{PlanName: "专业版", QuotaUsed: 100, QuotaRemaining: 200},
			{PlanName: "团队版", QuotaUsed: 300, QuotaRemaining: 400},
		},
	})

	raw, err := common.Marshal(notification.FeishuCard)
	require.NoError(t, err)
	card := string(raw)
	require.Contains(t, card, "今日 Token 用量提醒")
	require.Contains(t, card, "310 M")
	require.Contains(t, card, formatQuotaCNY(int64(common.QuotaPerUnit)))
	require.Contains(t, card, "专业版")
	require.Contains(t, card, "团队版")
	require.NotContains(t, card, "subscription_id")
	require.NotContains(t, card, "quota_limit")
}

func TestSubscriptionUsageNotificationCard(t *testing.T) {
	notification := buildSubscription80Notification(subscriptionUsageDigest{
		PlanName: "专业版", QuotaLimit: int64(common.QuotaPerUnit),
		QuotaUsed: int64(common.QuotaPerUnit * 0.8), QuotaRemaining: int64(common.QuotaPerUnit * 0.2),
		PeriodEnd: 1_788_681_600,
	})
	raw, err := common.Marshal(notification.FeishuCard)
	require.NoError(t, err)
	card := string(raw)
	require.Contains(t, card, "订阅额度温馨提醒")
	require.Contains(t, card, "80.0%")
	require.Contains(t, card, formatQuotaCNY(int64(common.QuotaPerUnit*0.2)))
}
