package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

func buildDailyTokenMilestoneNotification(payload dailyTokenMilestonePayload) dto.Notify {
	sections := []string{
		fmt.Sprintf("**今日累计 Token**\n%d M", payload.TokenUsed/1_000_000),
		fmt.Sprintf("**今日消耗金额**\n%s", formatQuotaCNY(payload.QuotaUsed)),
		fmt.Sprintf("**下一档提醒**\n%d M", payload.MilestoneM+100),
	}
	for _, subscription := range payload.Subscriptions {
		sections = append(sections, fmt.Sprintf(
			"**订阅计划：%s**\n本周期已用 %s · 剩余 %s",
			subscription.PlanName,
			formatQuotaCNY(subscription.QuotaUsed),
			formatQuotaCNY(subscription.QuotaRemaining),
		))
	}
	content := strings.Join(sections, "\n\n")
	return dto.Notify{
		Type: dto.NotifyTypeDailyTokenMilestone, Title: "今日 Token 用量提醒",
		Content:    content,
		FeishuCard: usageNotificationCard("今日 Token 用量提醒", fmt.Sprintf("已达到 %dM 档位", payload.MilestoneM), content, "blue"),
	}
}

func buildSubscription80Notification(payload subscriptionUsageDigest) dto.Notify {
	percent := float64(payload.QuotaUsed) / float64(payload.QuotaLimit) * 100
	periodEnd := "本周期结束时"
	if payload.PeriodEnd > 0 {
		periodEnd = time.Unix(payload.PeriodEnd, 0).In(beijingLocation).Format("2006-01-02 15:04")
	}
	content := fmt.Sprintf(
		"**订阅计划**\n%s\n\n**本周期已使用**\n%.1f%% · %s\n\n**剩余金额**\n%s\n\n**周期结束时间**\n%s",
		payload.PlanName, percent, formatQuotaCNY(payload.QuotaUsed),
		formatQuotaCNY(payload.QuotaRemaining), periodEnd,
	)
	return dto.Notify{
		Type: dto.NotifyTypeSubscriptionUsage80, Title: "订阅额度温馨提醒",
		Content:    content,
		FeishuCard: usageNotificationCard("订阅额度温馨提醒", "本周期额度已使用 80%", content, "orange"),
	}
}

func usageNotificationCard(title, subtitle, content, template string) map[string]any {
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": template,
			"title":    map[string]string{"tag": "plain_text", "content": title},
			"subtitle": map[string]string{"tag": "plain_text", "content": subtitle},
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": content}},
			map[string]any{"tag": "hr"},
			map[string]any{"tag": "note", "elements": []any{map[string]string{"tag": "plain_text", "content": "用量按北京时间统计"}}},
		},
	}
}
