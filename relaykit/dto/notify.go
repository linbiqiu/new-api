package dto

type Notify struct {
	Type       string        `json:"type"`
	Title      string        `json:"title"`
	Content    string        `json:"content"`
	Values     []interface{} `json:"values"`
	FeishuCard any           `json:"feishu_card,omitempty"`
}

const ContentValueParam = "{{value}}"

const (
	NotifyTypeQuotaExceed         = "quota_exceed"
	NotifyTypeChannelUpdate       = "channel_update"
	NotifyTypeChannelTest         = "channel_test"
	NotifyTypeDailyTokenMilestone = "daily_token_milestone"
	NotifyTypeSubscriptionUsage80 = "subscription_usage_80"
)

func NewNotify(t string, title string, content string, values []interface{}) Notify {
	return Notify{
		Type:    t,
		Title:   title,
		Content: content,
		Values:  values,
	}
}
