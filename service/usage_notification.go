package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const dailyTokenMilestoneSize int64 = 100_000_000

var beijingLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return location
}()

type subscriptionUsageDigest struct {
	SubscriptionID int    `json:"subscription_id"`
	PlanName       string `json:"plan_name"`
	PeriodStart    int64  `json:"period_start"`
	PeriodEnd      int64  `json:"period_end"`
	QuotaLimit     int64  `json:"quota_limit"`
	QuotaUsed      int64  `json:"quota_used"`
	QuotaRemaining int64  `json:"quota_remaining"`
}

type dailyTokenMilestonePayload struct {
	UsageDate     string                    `json:"usage_date"`
	MilestoneM    int64                     `json:"milestone_m"`
	TokenUsed     int64                     `json:"token_used"`
	QuotaUsed     int64                     `json:"quota_used"`
	Subscriptions []subscriptionUsageDigest `json:"subscriptions"`
}

func recordSettledDailyUsage(userID int, quotaUsed, tokenUsed int64, now time.Time, notifyEnabled bool) error {
	if quotaUsed < 0 || tokenUsed < 0 {
		return fmt.Errorf("settled usage must be non-negative")
	}
	usageDate := now.In(beijingLocation).Format("2006-01-02")
	counter, err := model.AddUserDailyUsage(userID, usageDate, quotaUsed, tokenUsed)
	if err != nil || !notifyEnabled || counter.TokenUsed < dailyTokenMilestoneSize {
		return err
	}

	milestoneM := counter.TokenUsed / dailyTokenMilestoneSize * 100
	subscriptions, err := activeSubscriptionUsageDigests(userID)
	if err != nil {
		return err
	}
	payload := dailyTokenMilestonePayload{
		UsageDate: usageDate, MilestoneM: milestoneM, TokenUsed: counter.TokenUsed,
		QuotaUsed: counter.QuotaUsed, Subscriptions: subscriptions,
	}
	payloadJSON, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = model.CreateNotificationEvent(&model.UserNotificationEvent{
		UserID: userID, EventType: model.NotificationEventDailyTokenMilestone,
		EventKey: fmt.Sprintf("%s:%d", usageDate, milestoneM), Payload: string(payloadJSON),
	})
	return err
}

func activeSubscriptionUsageDigests(userID int) ([]subscriptionUsageDigest, error) {
	summaries, err := model.GetAllActiveUserSubscriptions(userID)
	if err != nil {
		return nil, err
	}
	digests := make([]subscriptionUsageDigest, 0, len(summaries))
	for _, summary := range summaries {
		subscription := summary.Subscription
		if subscription == nil {
			continue
		}
		planName := fmt.Sprintf("套餐 #%d", subscription.PlanId)
		if plan, planErr := model.GetSubscriptionPlanInfoByUserSubscriptionId(subscription.Id); planErr == nil && plan != nil {
			planName = plan.PlanTitle
		}
		periodStart := subscription.LastResetTime
		if periodStart <= 0 {
			periodStart = subscription.StartTime
		}
		periodEnd := subscription.NextResetTime
		if periodEnd <= 0 {
			periodEnd = subscription.EndTime
		}
		remaining := subscription.AmountTotal - subscription.AmountUsed
		if remaining < 0 {
			remaining = 0
		}
		digests = append(digests, subscriptionUsageDigest{
			SubscriptionID: subscription.Id, PlanName: planName,
			PeriodStart: periodStart, PeriodEnd: periodEnd,
			QuotaLimit: subscription.AmountTotal, QuotaUsed: subscription.AmountUsed,
			QuotaRemaining: remaining,
		})
	}
	return digests, nil
}

func createSubscription80Event(userID int, digest subscriptionUsageDigest) (bool, error) {
	if userID <= 0 || digest.SubscriptionID <= 0 || digest.QuotaLimit <= 0 || digest.QuotaUsed < 0 {
		return false, nil
	}
	threshold := (digest.QuotaLimit/5)*4 + ((digest.QuotaLimit%5)*4+4)/5
	if digest.QuotaUsed < threshold {
		return false, nil
	}
	if digest.QuotaRemaining == 0 && digest.QuotaUsed < digest.QuotaLimit {
		digest.QuotaRemaining = digest.QuotaLimit - digest.QuotaUsed
	}
	if digest.QuotaRemaining < 0 {
		digest.QuotaRemaining = 0
	}
	payload, err := common.Marshal(digest)
	if err != nil {
		return false, err
	}
	return model.CreateNotificationEvent(&model.UserNotificationEvent{
		UserID: userID, EventType: model.NotificationEventSubscription80,
		EventKey: fmt.Sprintf("%d:%d:80", digest.SubscriptionID, digest.PeriodStart), Payload: string(payload),
	})
}

func subscriptionDigestFromBilling(session *BillingSession) (subscriptionUsageDigest, bool) {
	if session == nil {
		return subscriptionUsageDigest{}, false
	}
	funding, ok := session.funding.(*SubscriptionFunding)
	if !ok || funding.subscriptionId <= 0 {
		return subscriptionUsageDigest{}, false
	}
	used := funding.AmountUsedAfter
	if session.relayInfo != nil {
		used += session.relayInfo.SubscriptionPostDelta
	}
	remaining := funding.AmountTotal - used
	if remaining < 0 {
		remaining = 0
	}
	return subscriptionUsageDigest{
		SubscriptionID: funding.subscriptionId, PlanName: funding.PlanTitle,
		PeriodStart: funding.PeriodStart, PeriodEnd: funding.PeriodEnd,
		QuotaLimit: funding.AmountTotal, QuotaUsed: used, QuotaRemaining: remaining,
	}, true
}
