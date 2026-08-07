package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	NotificationEventDailyTokenMilestone = "daily_token_milestone"
	NotificationEventSubscription80      = "subscription_usage_80"

	NotificationStatusPending = "pending"
	NotificationStatusSending = "sending"
	NotificationStatusSent    = "sent"
	NotificationStatusFailed  = "failed"
)

const maxUsageCounterValue = int64(^uint64(0) >> 1)

type UserDailyUsageCounter struct {
	ID        int64  `json:"id" gorm:"primaryKey"`
	UserID    int    `json:"user_id" gorm:"uniqueIndex:idx_user_usage_day,priority:1"`
	UsageDate string `json:"usage_date" gorm:"type:varchar(10);uniqueIndex:idx_user_usage_day,priority:2"`
	QuotaUsed int64  `json:"quota_used" gorm:"type:bigint;not null"`
	TokenUsed int64  `json:"token_used" gorm:"type:bigint;not null"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
}

type UserNotificationEvent struct {
	ID          int64  `json:"id" gorm:"primaryKey"`
	UserID      int    `json:"user_id" gorm:"uniqueIndex:idx_user_event_key,priority:1;index"`
	EventType   string `json:"event_type" gorm:"type:varchar(48);uniqueIndex:idx_user_event_key,priority:2;index"`
	EventKey    string `json:"event_key" gorm:"type:varchar(128);uniqueIndex:idx_user_event_key,priority:3"`
	Payload     string `json:"payload" gorm:"type:text"`
	Status      string `json:"status" gorm:"type:varchar(16);index"`
	Attempts    int    `json:"attempts"`
	NextRetryAt int64  `json:"next_retry_at" gorm:"bigint;index"`
	LockedBy    string `json:"locked_by" gorm:"type:varchar(128);index"`
	LeaseUntil  int64  `json:"lease_until" gorm:"bigint;index"`
	LastError   string `json:"last_error" gorm:"type:text"`
	SentAt      int64  `json:"sent_at" gorm:"bigint"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`
}

func AddUserDailyUsage(userID int, usageDate string, quotaDelta, tokenDelta int64) (UserDailyUsageCounter, error) {
	if userID <= 0 || usageDate == "" {
		return UserDailyUsageCounter{}, fmt.Errorf("daily usage identity is invalid")
	}
	if quotaDelta < 0 || tokenDelta < 0 {
		return UserDailyUsageCounter{}, fmt.Errorf("daily usage delta must be non-negative")
	}

	now := common.GetTimestamp()
	row := UserDailyUsageCounter{UserID: userID, UsageDate: usageDate, QuotaUsed: quotaDelta, TokenUsed: tokenDelta, CreatedAt: now, UpdatedAt: now}
	err := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "usage_date"}},
		DoUpdates: clause.Assignments(map[string]any{
			"quota_used": gorm.Expr("CASE WHEN quota_used > ? THEN ? ELSE quota_used + ? END", maxUsageCounterValue-quotaDelta, maxUsageCounterValue, quotaDelta),
			"token_used": gorm.Expr("CASE WHEN token_used > ? THEN ? ELSE token_used + ? END", maxUsageCounterValue-tokenDelta, maxUsageCounterValue, tokenDelta),
			"updated_at": now,
		}),
	}).Create(&row).Error
	if err != nil {
		return UserDailyUsageCounter{}, err
	}
	return GetUserDailyUsage(userID, usageDate)
}

func GetUserDailyUsage(userID int, usageDate string) (UserDailyUsageCounter, error) {
	var usage UserDailyUsageCounter
	err := DB.Where("user_id = ? AND usage_date = ?", userID, usageDate).First(&usage).Error
	return usage, err
}

func CreateNotificationEvent(event *UserNotificationEvent) (bool, error) {
	if event == nil || event.UserID <= 0 || event.EventType == "" || event.EventKey == "" {
		return false, fmt.Errorf("notification event identity is invalid")
	}
	now := common.GetTimestamp()
	event.Status = NotificationStatusPending
	event.CreatedAt = now
	event.UpdatedAt = now
	result := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(event)
	return result.RowsAffected == 1, result.Error
}

func FindDueNotificationEvents(now int64, limit int) ([]UserNotificationEvent, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	var events []UserNotificationEvent
	err := DB.Where(
		"(status = ? AND next_retry_at <= ?) OR (status = ? AND lease_until < ?)",
		NotificationStatusPending, now, NotificationStatusSending, now,
	).Order("id asc").Limit(limit).Find(&events).Error
	return events, err
}

func ClaimNotificationEvent(id int64, runnerID string, now, leaseUntil int64) (*UserNotificationEvent, bool, error) {
	if id <= 0 || strings.TrimSpace(runnerID) == "" || leaseUntil <= now {
		return nil, false, fmt.Errorf("notification event claim is invalid")
	}
	result := DB.Model(&UserNotificationEvent{}).
		Where("id = ? AND ((status = ? AND next_retry_at <= ?) OR (status = ? AND lease_until < ?))",
			id, NotificationStatusPending, now, NotificationStatusSending, now).
		Updates(map[string]any{
			"status": NotificationStatusSending, "locked_by": runnerID,
			"lease_until": leaseUntil, "attempts": gorm.Expr("attempts + 1"), "updated_at": now,
		})
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, false, result.Error
	}
	var event UserNotificationEvent
	if err := DB.First(&event, id).Error; err != nil {
		return nil, false, err
	}
	return &event, true, nil
}

func MarkNotificationEventSent(id int64, runnerID string, now int64) (bool, error) {
	result := DB.Model(&UserNotificationEvent{}).
		Where("id = ? AND status = ? AND locked_by = ?", id, NotificationStatusSending, runnerID).
		Updates(map[string]any{
			"status": NotificationStatusSent, "sent_at": now, "locked_by": "",
			"lease_until": 0, "last_error": "", "updated_at": now,
		})
	return result.RowsAffected == 1, result.Error
}

func MarkNotificationEventFailed(id int64, runnerID string, final bool, nextRetryAt, now int64, lastError string) (bool, error) {
	if len(lastError) > 1000 {
		lastError = lastError[:1000]
	}
	status := NotificationStatusPending
	if final {
		status = NotificationStatusFailed
	}
	result := DB.Model(&UserNotificationEvent{}).
		Where("id = ? AND status = ? AND locked_by = ?", id, NotificationStatusSending, runnerID).
		Updates(map[string]any{
			"status": status, "next_retry_at": nextRetryAt, "locked_by": "",
			"lease_until": 0, "last_error": lastError, "updated_at": now,
		})
	return result.RowsAffected == 1, result.Error
}

func CountNotificationEventsByStatus(status string) (int64, error) {
	var count int64
	err := DB.Model(&UserNotificationEvent{}).Where("status = ?", status).Count(&count).Error
	return count, err
}
