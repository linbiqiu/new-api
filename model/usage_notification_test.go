package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupUsageNotificationTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&UserDailyUsageCounter{}, &UserNotificationEvent{}))
	require.NoError(t, DB.Exec("DELETE FROM user_daily_usage_counters").Error)
	require.NoError(t, DB.Exec("DELETE FROM user_notification_events").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_daily_usage_counters")
		DB.Exec("DELETE FROM user_notification_events")
	})
}

func TestAddUserDailyUsageAccumulatesBothMetrics(t *testing.T) {
	setupUsageNotificationTest(t)

	first, err := AddUserDailyUsage(42, "2026-08-06", 1200, 60_000_000)
	require.NoError(t, err)
	require.EqualValues(t, 60_000_000, first.TokenUsed)
	second, err := AddUserDailyUsage(42, "2026-08-06", 800, 50_000_000)
	require.NoError(t, err)
	require.EqualValues(t, 110_000_000, second.TokenUsed)

	usage, err := GetUserDailyUsage(42, "2026-08-06")
	require.NoError(t, err)
	require.EqualValues(t, 2000, usage.QuotaUsed)
	require.EqualValues(t, 110_000_000, usage.TokenUsed)
}

func TestAddUserDailyUsageRejectsNegativeDelta(t *testing.T) {
	setupUsageNotificationTest(t)

	_, err := AddUserDailyUsage(42, "2026-08-06", -1, 0)
	require.Error(t, err)
	_, err = AddUserDailyUsage(42, "2026-08-06", 0, -1)
	require.Error(t, err)
}

func TestAddUserDailyUsageSaturatesInsteadOfOverflowing(t *testing.T) {
	setupUsageNotificationTest(t)
	require.NoError(t, DB.Create(&UserDailyUsageCounter{
		UserID: 42, UsageDate: "2026-08-06",
		QuotaUsed: maxUsageCounterValue - 1, TokenUsed: maxUsageCounterValue - 1,
	}).Error)

	usage, err := AddUserDailyUsage(42, "2026-08-06", 2, 2)
	require.NoError(t, err)
	require.Equal(t, maxUsageCounterValue, usage.QuotaUsed)
	require.Equal(t, maxUsageCounterValue, usage.TokenUsed)
}

func TestCreateNotificationEventIsIdempotent(t *testing.T) {
	setupUsageNotificationTest(t)

	event := UserNotificationEvent{UserID: 42, EventType: NotificationEventDailyTokenMilestone, EventKey: "2026-08-06:100", Payload: `{}`}
	created, err := CreateNotificationEvent(&event)
	require.NoError(t, err)
	require.True(t, created)

	event.ID = 0
	created, err = CreateNotificationEvent(&event)
	require.NoError(t, err)
	require.False(t, created)
}

func TestCreateNotificationEventConcurrentIsIdempotent(t *testing.T) {
	setupUsageNotificationTest(t)

	var wg sync.WaitGroup
	created := make(chan bool, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			event := UserNotificationEvent{UserID: 84, EventType: NotificationEventDailyTokenMilestone, EventKey: "2026-08-06:200", Payload: `{}`}
			ok, err := CreateNotificationEvent(&event)
			require.NoError(t, err)
			created <- ok
		}()
	}
	wg.Wait()
	close(created)

	createdCount := 0
	for ok := range created {
		if ok {
			createdCount++
		}
	}
	require.Equal(t, 1, createdCount)
}

func TestNotificationEventClaimUsesLeaseAndCAS(t *testing.T) {
	setupUsageNotificationTest(t)
	event := UserNotificationEvent{UserID: 42, EventType: NotificationEventDailyTokenMilestone, EventKey: "2026-08-06:100", Payload: `{}`}
	created, err := CreateNotificationEvent(&event)
	require.NoError(t, err)
	require.True(t, created)

	claimed, ok, err := ClaimNotificationEvent(event.ID, "runner-a", 100, 220)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, claimed.Attempts)
	_, ok, err = ClaimNotificationEvent(event.ID, "runner-b", 101, 221)
	require.NoError(t, err)
	require.False(t, ok)

	reclaimed, ok, err := ClaimNotificationEvent(event.ID, "runner-b", 221, 341)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2, reclaimed.Attempts)
	marked, err := MarkNotificationEventSent(event.ID, "runner-a", 222)
	require.NoError(t, err)
	require.False(t, marked)
	marked, err = MarkNotificationEventSent(event.ID, "runner-b", 222)
	require.NoError(t, err)
	require.True(t, marked)
}

func TestNotificationEventFailureTransitions(t *testing.T) {
	setupUsageNotificationTest(t)
	event := UserNotificationEvent{UserID: 42, EventType: NotificationEventSubscription80, EventKey: "7:100:80", Payload: `{}`}
	_, err := CreateNotificationEvent(&event)
	require.NoError(t, err)
	claimed, ok, err := ClaimNotificationEvent(event.ID, "runner", 100, 220)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, claimed.Attempts)

	marked, err := MarkNotificationEventFailed(event.ID, "runner", false, 160, 101, "temporary")
	require.NoError(t, err)
	require.True(t, marked)
	due, err := FindDueNotificationEvents(159, 50)
	require.NoError(t, err)
	require.Empty(t, due)
	due, err = FindDueNotificationEvents(160, 50)
	require.NoError(t, err)
	require.Len(t, due, 1)
}
