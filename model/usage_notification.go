package model

import (
	"fmt"

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
			"quota_used": gorm.Expr("quota_used + ?", quotaDelta),
			"token_used": gorm.Expr("token_used + ?", tokenDelta),
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
