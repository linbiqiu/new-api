package service

import "sync/atomic"

type UsageGovernanceMetricsSnapshot struct {
	Checks            uint64 `json:"checks"`
	Rejected          uint64 `json:"rejected"`
	RedisFallbacks    uint64 `json:"redis_fallbacks"`
	Unavailable       uint64 `json:"unavailable"`
	CounterWriteError uint64 `json:"counter_write_errors"`
}

var usageGovernanceMetrics struct {
	checks            atomic.Uint64
	rejected          atomic.Uint64
	redisFallbacks    atomic.Uint64
	unavailable       atomic.Uint64
	counterWriteError atomic.Uint64
}

func RecordUsageGovernanceCheck()         { usageGovernanceMetrics.checks.Add(1) }
func RecordUsageGovernanceRejected()      { usageGovernanceMetrics.rejected.Add(1) }
func RecordUsageGovernanceRedisFallback() { usageGovernanceMetrics.redisFallbacks.Add(1) }
func RecordUsageGovernanceUnavailable()   { usageGovernanceMetrics.unavailable.Add(1) }
func RecordUsageGovernanceCounterError()  { usageGovernanceMetrics.counterWriteError.Add(1) }

func SnapshotUsageGovernanceMetrics() UsageGovernanceMetricsSnapshot {
	return UsageGovernanceMetricsSnapshot{
		Checks: usageGovernanceMetrics.checks.Load(), Rejected: usageGovernanceMetrics.rejected.Load(),
		RedisFallbacks: usageGovernanceMetrics.redisFallbacks.Load(), Unavailable: usageGovernanceMetrics.unavailable.Load(),
		CounterWriteError: usageGovernanceMetrics.counterWriteError.Load(),
	}
}
