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
