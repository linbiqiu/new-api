package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestUsageNotificationDeliveryContinuesAfterFailure(t *testing.T) {
	setupUsageNotificationServiceTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}))
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := model.User{Username: "notify-" + suffix, Password: "password123", Email: "user@example.com", AffCode: "n" + suffix, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	t.Cleanup(func() { model.DB.Delete(&user) })

	for index := 1; index <= 2; index++ {
		payload, err := common.Marshal(dailyTokenMilestonePayload{UsageDate: "2026-08-06", MilestoneM: int64(index * 100), TokenUsed: int64(index) * 100_000_000})
		require.NoError(t, err)
		created, err := model.CreateNotificationEvent(&model.UserNotificationEvent{UserID: user.Id, EventType: model.NotificationEventDailyTokenMilestone, EventKey: fmt.Sprintf("2026-08-06:%d", index*100), Payload: string(payload)})
		require.NoError(t, err)
		require.True(t, created)
	}

	calls := 0
	summary, err := deliverUsageNotifications(context.Background(), "runner-test", time.Unix(common.GetTimestamp(), 0), func(_ int, _ string, _ dto.UserSetting, _ dto.Notify) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("temporary send failure")
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, summary.Claimed)
	require.Equal(t, 1, summary.Retried)
	require.Equal(t, 1, summary.Sent)
}

func TestUsageNotificationDeliveryMarksFifthFailureFinal(t *testing.T) {
	setupUsageNotificationServiceTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}))
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := model.User{Username: "notify-final-" + suffix, Password: "password123", AffCode: "f" + suffix, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	t.Cleanup(func() { model.DB.Delete(&user) })
	payload, err := common.Marshal(subscriptionUsageDigest{SubscriptionID: 7, PlanName: "专业版", PeriodStart: 100, QuotaLimit: 100, QuotaUsed: 80, QuotaRemaining: 20})
	require.NoError(t, err)
	event := model.UserNotificationEvent{UserID: user.Id, EventType: model.NotificationEventSubscription80, EventKey: "7:100:80", Payload: string(payload), Attempts: 4}
	created, err := model.CreateNotificationEvent(&event)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, model.DB.Model(&event).Update("attempts", 4).Error)

	now := time.Unix(common.GetTimestamp(), 0)
	summary, err := deliverUsageNotifications(context.Background(), "runner-final", now, func(int, string, dto.UserSetting, dto.Notify) error {
		return fmt.Errorf("permanent failure")
	})
	require.NoError(t, err)
	require.Equal(t, 1, summary.Failed)
	var stored model.UserNotificationEvent
	require.NoError(t, model.DB.First(&stored, event.ID).Error)
	require.Equal(t, model.NotificationStatusFailed, stored.Status)
	require.Equal(t, 5, stored.Attempts)
}
