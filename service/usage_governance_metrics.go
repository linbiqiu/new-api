package service

import (
	"sync/atomic"

	"github.com/QuantumNous/new-api/model"
)

type UsageGovernanceMetricsSnapshot struct {
	Checks                       uint64 `json:"checks"`
	Rejected                     uint64 `json:"rejected"`
	RedisFallbacks               uint64 `json:"redis_fallbacks"`
	Unavailable                  uint64 `json:"unavailable"`
	CounterWriteError            uint64 `json:"counter_write_errors"`
	NotificationPending          int64  `json:"notification_pending"`
	NotificationSent             uint64 `json:"notification_sent"`
	NotificationFailed           uint64 `json:"notification_failed"`
	NotificationRetries          uint64 `json:"notification_retries"`
	NotificationLastDelaySeconds uint64 `json:"notification_last_delay_seconds"`
}

var usageGovernanceMetrics struct {
	checks                       atomic.Uint64
	rejected                     atomic.Uint64
	redisFallbacks               atomic.Uint64
	unavailable                  atomic.Uint64
	counterWriteError            atomic.Uint64
	notificationSent             atomic.Uint64
	notificationFailed           atomic.Uint64
	notificationRetries          atomic.Uint64
	notificationLastDelaySeconds atomic.Uint64
}

func RecordUsageGovernanceCheck()         { usageGovernanceMetrics.checks.Add(1) }
func RecordUsageGovernanceRejected()      { usageGovernanceMetrics.rejected.Add(1) }
func RecordUsageGovernanceRedisFallback() { usageGovernanceMetrics.redisFallbacks.Add(1) }
func RecordUsageGovernanceUnavailable()   { usageGovernanceMetrics.unavailable.Add(1) }
func RecordUsageGovernanceCounterError()  { usageGovernanceMetrics.counterWriteError.Add(1) }
func RecordUsageNotificationSent(delaySeconds uint64) {
	usageGovernanceMetrics.notificationSent.Add(1)
	usageGovernanceMetrics.notificationLastDelaySeconds.Store(delaySeconds)
}
func RecordUsageNotificationFailed() { usageGovernanceMetrics.notificationFailed.Add(1) }
func RecordUsageNotificationRetry()  { usageGovernanceMetrics.notificationRetries.Add(1) }

func SnapshotUsageGovernanceMetrics() UsageGovernanceMetricsSnapshot {
	pending, _ := model.CountNotificationEventsByStatus(model.NotificationStatusPending)
	return UsageGovernanceMetricsSnapshot{
		Checks: usageGovernanceMetrics.checks.Load(), Rejected: usageGovernanceMetrics.rejected.Load(),
		RedisFallbacks: usageGovernanceMetrics.redisFallbacks.Load(), Unavailable: usageGovernanceMetrics.unavailable.Load(),
		CounterWriteError:            usageGovernanceMetrics.counterWriteError.Load(),
		NotificationPending:          pending,
		NotificationSent:             usageGovernanceMetrics.notificationSent.Load(),
		NotificationFailed:           usageGovernanceMetrics.notificationFailed.Load(),
		NotificationRetries:          usageGovernanceMetrics.notificationRetries.Load(),
		NotificationLastDelaySeconds: usageGovernanceMetrics.notificationLastDelaySeconds.Load(),
	}
}
