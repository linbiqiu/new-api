package dto

import "testing"

func TestDailyTokenMilestoneNotifyEnabledDefaultsToTrue(t *testing.T) {
	if !(UserSetting{}).DailyTokenMilestoneNotifyEnabled() {
		t.Fatal("unset preference must default to enabled")
	}
	disabled := false
	if (UserSetting{DailyTokenMilestoneNotify: &disabled}).DailyTokenMilestoneNotifyEnabled() {
		t.Fatal("explicit false must disable daily milestone notifications")
	}
}
