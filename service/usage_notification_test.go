package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func setupUsageNotificationServiceTest(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.UserDailyUsageCounter{}, &model.UserNotificationEvent{}))
	require.NoError(t, model.DB.Exec("DELETE FROM user_daily_usage_counters").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM user_notification_events").Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM user_daily_usage_counters")
		model.DB.Exec("DELETE FROM user_notification_events")
	})
}

func listUsageNotificationEventsForTest(t *testing.T, userID int) []model.UserNotificationEvent {
	t.Helper()
	var events []model.UserNotificationEvent
	require.NoError(t, model.DB.Where("user_id = ?", userID).Order("id asc").Find(&events).Error)
	return events
}

func TestRecordSettledDailyUsageCreatesOnlyHighestCrossedMilestone(t *testing.T) {
	setupUsageNotificationServiceTest(t)
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, beijingLocation)

	require.NoError(t, recordSettledDailyUsage(42, 900, 90_000_000, now, true))
	require.NoError(t, recordSettledDailyUsage(42, 2200, 220_000_000, now, true))

	events := listUsageNotificationEventsForTest(t, 42)
	require.Len(t, events, 1)
	require.Equal(t, "2026-08-06:300", events[0].EventKey)

	require.NoError(t, recordSettledDailyUsage(42, 100, 90_000_000, now, true))
	events = listUsageNotificationEventsForTest(t, 42)
	require.Len(t, events, 2)
	require.Equal(t, "2026-08-06:400", events[1].EventKey)
}

func TestRecordSettledDailyUsageUsesBeijingDateAndHonorsSwitch(t *testing.T) {
	setupUsageNotificationServiceTest(t)
	beforeMidnightUTC := time.Date(2026, 8, 6, 15, 59, 0, 0, time.UTC)
	afterMidnightUTC := beforeMidnightUTC.Add(2 * time.Minute)

	require.NoError(t, recordSettledDailyUsage(43, 100, 100_000_000, beforeMidnightUTC, false))
	require.NoError(t, recordSettledDailyUsage(43, 200, 100_000_000, afterMidnightUTC, true))

	first, err := model.GetUserDailyUsage(43, "2026-08-06")
	require.NoError(t, err)
	require.EqualValues(t, 100_000_000, first.TokenUsed)
	second, err := model.GetUserDailyUsage(43, "2026-08-07")
	require.NoError(t, err)
	require.EqualValues(t, 100_000_000, second.TokenUsed)
	events := listUsageNotificationEventsForTest(t, 43)
	require.Len(t, events, 1)
	require.Equal(t, "2026-08-07:100", events[0].EventKey)
}

func TestCreateSubscription80EventOncePerCycle(t *testing.T) {
	setupUsageNotificationServiceTest(t)
	usage := subscriptionUsageDigest{SubscriptionID: 7, PlanName: "专业版", PeriodStart: 1_786_003_200, PeriodEnd: 1_788_681_600, QuotaLimit: 100_000, QuotaUsed: 80_000}

	created, err := createSubscription80Event(42, usage)
	require.NoError(t, err)
	require.True(t, created)
	created, err = createSubscription80Event(42, usage)
	require.NoError(t, err)
	require.False(t, created)

	usage.PeriodStart++
	created, err = createSubscription80Event(42, usage)
	require.NoError(t, err)
	require.True(t, created)
}

func TestCreateSubscription80EventThresholdAndUnlimited(t *testing.T) {
	setupUsageNotificationServiceTest(t)
	base := subscriptionUsageDigest{SubscriptionID: 8, PeriodStart: common.GetTimestamp(), QuotaLimit: 100_000, QuotaUsed: 79_999}

	created, err := createSubscription80Event(42, base)
	require.NoError(t, err)
	require.False(t, created)
	base.QuotaUsed = 80_000
	created, err = createSubscription80Event(42, base)
	require.NoError(t, err)
	require.True(t, created)
	base.SubscriptionID = 9
	base.QuotaLimit = 0
	created, err = createSubscription80Event(42, base)
	require.NoError(t, err)
	require.False(t, created)
}
