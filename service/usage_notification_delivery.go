package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

var usageNotificationRetryDelays = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	time.Hour,
	6 * time.Hour,
}

type UsageNotificationDeliverySummary struct {
	Claimed int `json:"claimed"`
	Sent    int `json:"sent"`
	Retried int `json:"retried"`
	Failed  int `json:"failed"`
}

type usageNotificationSender func(int, string, dto.UserSetting, dto.Notify) error

func DeliverUsageNotifications(ctx context.Context, runnerID string) (UsageNotificationDeliverySummary, error) {
	return deliverUsageNotifications(ctx, runnerID, time.Now(), NotifyUser)
}

func deliverUsageNotifications(ctx context.Context, runnerID string, now time.Time, sender usageNotificationSender) (UsageNotificationDeliverySummary, error) {
	summary := UsageNotificationDeliverySummary{}
	nowUnix := now.Unix()
	events, err := model.FindDueNotificationEvents(nowUnix, 50)
	if err != nil {
		return summary, err
	}
	for _, candidate := range events {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		event, claimed, err := model.ClaimNotificationEvent(candidate.ID, runnerID, nowUnix, nowUnix+120)
		if err != nil {
			return summary, err
		}
		if !claimed {
			continue
		}
		summary.Claimed++

		deliveryErr := deliverUsageNotificationEvent(event, sender)
		if deliveryErr == nil {
			marked, markErr := model.MarkNotificationEventSent(event.ID, runnerID, nowUnix)
			if markErr != nil {
				return summary, markErr
			}
			if marked {
				summary.Sent++
				RecordUsageNotificationSent(uint64(max(nowUnix-event.CreatedAt, 0)))
			}
			continue
		}

		final := event.Attempts >= len(usageNotificationRetryDelays)
		nextRetryAt := nowUnix
		if !final {
			nextRetryAt += int64(usageNotificationRetryDelays[event.Attempts-1].Seconds())
		}
		errorSummary := strings.TrimSpace(deliveryErr.Error())
		marked, markErr := model.MarkNotificationEventFailed(event.ID, runnerID, final, nextRetryAt, nowUnix, errorSummary)
		if markErr != nil {
			return summary, markErr
		}
		if !marked {
			continue
		}
		if final {
			summary.Failed++
			RecordUsageNotificationFailed()
			common.SysLog(fmt.Sprintf("usage notification permanently failed: event_id=%d type=%s key=%s attempt=%d", event.ID, event.EventType, event.EventKey, event.Attempts))
		} else {
			summary.Retried++
			RecordUsageNotificationRetry()
			common.SysLog(fmt.Sprintf("usage notification will retry: event_id=%d type=%s key=%s attempt=%d", event.ID, event.EventType, event.EventKey, event.Attempts))
		}
	}
	return summary, nil
}

func deliverUsageNotificationEvent(event *model.UserNotificationEvent, sender usageNotificationSender) error {
	if event == nil {
		return fmt.Errorf("notification event is nil")
	}
	user, err := model.GetUserById(event.UserID, true)
	if err != nil {
		return fmt.Errorf("query notification recipient: %w", err)
	}

	var notification dto.Notify
	switch event.EventType {
	case model.NotificationEventDailyTokenMilestone:
		var payload dailyTokenMilestonePayload
		if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode daily usage payload: %w", err)
		}
		notification = buildDailyTokenMilestoneNotification(payload)
	case model.NotificationEventSubscription80:
		var payload subscriptionUsageDigest
		if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode subscription usage payload: %w", err)
		}
		notification = buildSubscription80Notification(payload)
	default:
		return fmt.Errorf("unsupported usage notification type: %s", event.EventType)
	}
	return sender(user.Id, user.Email, user.GetSetting(), notification)
}
