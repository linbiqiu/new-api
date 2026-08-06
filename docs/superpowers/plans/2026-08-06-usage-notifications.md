# 用户用量提醒与飞书卡片 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按用户记录北京时间每日实际 Token 与人民币消耗，在每个 100M 档位和订阅周期达到 80% 时创建幂等通知，并按用户渠道可靠投递方案 B 飞书卡片。

**Architecture:** 结算事务完成后同步原子累计每日用量，并用唯一事件键写入持久化 outbox；多节点系统任务通过租约批量投递。用户未设置渠道时默认飞书，只有在发送请求前即可确定飞书未配置或用户未绑定时回退邮箱；显式渠道不回退。

**Tech Stack:** Go 1.22+、Gin、GORM v2、React 19、TypeScript、飞书开放平台消息 API、Bun

**Canonical spec:** `docs/superpowers/specs/2026-08-06-user-usage-limits-and-notifications-design.md`

**Prerequisite:** 完成 `docs/superpowers/plans/2026-08-06-user-usage-limits-core.md`，复用其中的实际结算金额、Token、订阅实例和北京时间周期信息。

---

### Task 1: 增加每日计数器和通知 outbox 模型

**Files:**
- Create: `model/usage_notification.go`
- Create: `model/usage_notification_test.go`
- Modify: `model/main.go`

- [ ] **Step 1: 写失败测试覆盖原子累计与事件幂等**

测试必须覆盖：同一用户同一天只有一个计数器；金额和 Token 同时累加并返回更新后的快照；任一负增量被拒绝；相同事件键并发创建只成功一次；不同用户或不同周期互不影响。

```go
func TestAddUserDailyUsageAccumulatesBothMetrics(t *testing.T) {
	day := "2026-08-06"
	first, err := AddUserDailyUsage(42, day, 1200, 60_000_000)
	require.NoError(t, err)
	require.EqualValues(t, 60_000_000, first.TokenUsed)
	second, err := AddUserDailyUsage(42, day, 800, 50_000_000)
	require.NoError(t, err)
	require.EqualValues(t, 110_000_000, second.TokenUsed)

	usage, err := GetUserDailyUsage(42, day)
	require.NoError(t, err)
	require.EqualValues(t, 2000, usage.QuotaUsed)
	require.EqualValues(t, 110_000_000, usage.TokenUsed)
}

func TestCreateNotificationEventIsIdempotent(t *testing.T) {
	event := UserNotificationEvent{
		UserID: 42, EventType: NotificationEventDailyTokenMilestone,
		EventKey: "2026-08-06:100", Payload: `{}`,
	}
	created, err := CreateNotificationEvent(&event)
	require.NoError(t, err)
	require.True(t, created)

	event.ID = 0
	created, err = CreateNotificationEvent(&event)
	require.NoError(t, err)
	require.False(t, created)
}
```

- [ ] **Step 2: 运行测试并确认缺少模型而失败**

Run: `go test ./model -run 'UserDailyUsage|NotificationEvent' -count=1`

Expected: FAIL，提示计数器、事件类型或方法未定义。

- [ ] **Step 3: 定义跨数据库兼容的数据结构**

在 `model/usage_notification.go` 定义：

```go
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
	ID            int64  `json:"id" gorm:"primaryKey"`
	UserID        int    `json:"user_id" gorm:"uniqueIndex:idx_user_event_key,priority:1;index"`
	EventType     string `json:"event_type" gorm:"type:varchar(48);uniqueIndex:idx_user_event_key,priority:2;index"`
	EventKey      string `json:"event_key" gorm:"type:varchar(128);uniqueIndex:idx_user_event_key,priority:3"`
	Payload       string `json:"payload" gorm:"type:text"`
	Status        string `json:"status" gorm:"type:varchar(16);index"`
	Attempts      int    `json:"attempts"`
	NextRetryAt   int64  `json:"next_retry_at" gorm:"bigint;index"`
	LockedBy      string `json:"locked_by" gorm:"type:varchar(128);index"`
	LeaseUntil    int64  `json:"lease_until" gorm:"bigint;index"`
	LastError     string `json:"last_error" gorm:"type:text"`
	SentAt        int64  `json:"sent_at" gorm:"bigint"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint"`
}
```

不要使用数据库专属 JSON 类型；payload 必须通过 `common.Marshal` / `common.Unmarshal` 处理。将两个实体加入 `model/main.go` 的 AutoMigrate 列表，保留该文件中用户现有未提交改动。

- [ ] **Step 4: 实现原子累计和幂等创建**

`AddUserDailyUsage` 拒绝负数，使用 GORM `clause.OnConflict` 对 `(user_id, usage_date)` 执行原子加法，再读取并返回当前累计快照。后续读取可能包含同时完成的其他请求，这是允许的：事件键只取当前最高 100M 档并保持幂等，因此并发合并只会竞争同一个最高档事件，不会重复投递。`CreateNotificationEvent` 使用 `clause.OnConflict{DoNothing: true}`，再由 `RowsAffected == 1` 返回是否首次创建。

```go
func AddUserDailyUsage(userID int, usageDate string, quotaDelta, tokenDelta int64) (UserDailyUsageCounter, error) {
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
	var current UserDailyUsageCounter
	err = DB.Where("user_id = ? AND usage_date = ?", userID, usageDate).First(&current).Error
	return current, err
}
```

- [ ] **Step 5: 运行测试并提交模型切片**

Run: `go test ./model -run 'UserDailyUsage|NotificationEvent' -count=1`

Expected: PASS。

```bash
git add model/usage_notification.go model/usage_notification_test.go model/main.go
git commit -m "feat: add usage notification outbox models"
```

---

### Task 2: 从实际结算生成每日 Token 里程碑事件

**Files:**
- Create: `service/usage_notification.go`
- Create: `service/usage_notification_test.go`
- Modify: `service/billing.go`
- Modify: `service/text_quota.go`
- Modify: `service/quota.go`
- Modify: `controller/relay.go`

- [ ] **Step 1: 写失败测试覆盖北京时间与跨档合并**

通过注入固定时间，测试：99M 不创建事件；99M 增至 100M 创建 `100`；90M 增至 310M 只创建 `300`；后续达到 400M 再创建；北京时间跨日重新计算；关闭开关后仍记用量但不创建里程碑。

```go
func TestRecordSettledDailyUsageCreatesOnlyHighestCrossedMilestone(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, beijingLocation)
	require.NoError(t, recordSettledDailyUsage(42, 900, 90_000_000, now, true))
	require.NoError(t, recordSettledDailyUsage(42, 2200, 220_000_000, now, true))

	events := listUsageNotificationEventsForTest(t, 42)
	require.Len(t, events, 1)
	require.Equal(t, "2026-08-06:300", events[0].EventKey)
}
```

在同一测试文件中定义 `listUsageNotificationEventsForTest`，直接用测试数据库按 `user_id` 排序查询，避免依赖生产测试钩子。

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./service -run 'SettledDailyUsage|DailyTokenMilestone' -count=1`

Expected: FAIL，提示记录函数或 payload 不存在。

- [ ] **Step 3: 定义稳定 payload 和档位计算**

```go
const dailyTokenMilestoneSize int64 = 100_000_000

type dailyTokenMilestonePayload struct {
	UsageDate       string                    `json:"usage_date"`
	MilestoneM      int64                     `json:"milestone_m"`
	TokenUsed       int64                     `json:"token_used"`
	QuotaUsed       int64                     `json:"quota_used"`
	Subscriptions   []subscriptionUsageDigest `json:"subscriptions"`
}
```

调用 `AddUserDailyUsage` 取得更新后的累计快照。若 `TokenUsed >= dailyTokenMilestoneSize`，计算当前最高档并尝试创建事件；唯一事件键使同档重复结算无副作用。事件键使用 `YYYY-MM-DD:<milestone_m>`，其中 `milestone_m` 为 `100、200、300...`。单次跨多档只写最高档；较低档不补发。

金额字段保存内部 quota 的非负 `int64`，只在渲染通知时转换人民币。订阅摘要在事件创建时按实际订阅实例逐条快照，避免异步投递时周期变化导致内容不一致。

- [ ] **Step 4: 接入统一结算观察点**

在核心计划新增的 `SettleBilling` 实际金额和 Token 入口中，同步调用 `recordSettledDailyUsage`。调用点只传本次实际非负消费；退款或任务冲正不生成负每日消费。若上游已成功但计数失败，写请求相关错误和审计标记，后续额度检查因数据不可确认而 fail-closed。

所有调用方必须明确传递 input + output Token；拿不到 Token 的异步任务传 0，但仍累计金额。不要通过日志表异步反推每日用量。

- [ ] **Step 5: 运行定向测试并提交里程碑切片**

Run: `go test ./service ./controller -run 'SettledDailyUsage|DailyTokenMilestone|SettleBilling' -count=1`

Expected: PASS。

```bash
git add service/usage_notification.go service/usage_notification_test.go service/billing.go service/text_quota.go service/quota.go controller/relay.go
git commit -m "feat: create daily token milestone events"
```

---

### Task 3: 为每个订阅周期生成一次 80% 提醒

**Files:**
- Modify: `service/usage_notification.go`
- Modify: `service/usage_notification_test.go`
- Modify: `service/quota.go`
- Modify: `service/notify-limit.go`

- [ ] **Step 1: 写失败测试覆盖周期幂等和多订阅隔离**

覆盖 79% 不提醒、跨至 80% 创建一次、同周期后续不重复、管理员重置后的新周期再次创建、无限额度不提醒、两个订阅分别创建事件。

```go
func TestCreateSubscription80EventOncePerCycle(t *testing.T) {
	usage := subscriptionUsageDigest{SubscriptionID: 7, PlanName: "专业版", PeriodStart: 1_786_003_200, PeriodEnd: 1_788_681_600, QuotaLimit: 100_000, QuotaUsed: 80_000}
	created, err := createSubscription80Event(42, usage)
	require.NoError(t, err)
	require.True(t, created)
	created, err = createSubscription80Event(42, usage)
	require.NoError(t, err)
	require.False(t, created)
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./service -run Subscription80 -count=1`

Expected: FAIL，提示事件生成函数不存在。

- [ ] **Step 3: 接入实际出资订阅并替换旧提醒**

仅对 `BillingSession.FundingSource` 中本次实际扣费的订阅实例计算：

```go
if digest.QuotaLimit <= 0 || digest.QuotaUsed*100 < digest.QuotaLimit*80 {
	return false, nil
}
eventKey := fmt.Sprintf("%d:%d:80", digest.SubscriptionID, digest.PeriodStart)
```

乘法比较前使用安全顺序或 `math/big`/商余比较，避免 `int64` 溢出；不得直接采用无界 `QuotaUsed*100`。payload 包含套餐名、周期起止、已用、上限和剩余金额。剩余量取 `max(limit-used, 0)`。

移除该路径对旧 `checkAndSendSubscriptionQuotaNotify` 的直接发送；`service/notify-limit.go` 保留其他通知限流用途，不让每日限流吞掉新周期 outbox 事件。

- [ ] **Step 4: 运行测试并提交订阅提醒切片**

Run: `go test ./service -run 'Subscription80|SubscriptionQuotaNotify' -count=1`

Expected: PASS，且旧的重复提醒测试按新语义更新。

```bash
git add service/usage_notification.go service/usage_notification_test.go service/quota.go service/notify-limit.go
git commit -m "feat: enqueue subscription quota reminders"
```

---

### Task 4: 实现多节点安全的 outbox 投递工作器

**Files:**
- Modify: `model/usage_notification.go`
- Modify: `model/usage_notification_test.go`
- Modify: `model/system_task.go`
- Create: `service/usage_notification_delivery.go`
- Create: `service/usage_notification_delivery_test.go`
- Modify: `controller/system_task_handlers.go`
- Modify: `controller/system_task_handlers_test.go`

- [ ] **Step 1: 写失败测试覆盖租约、重试和最终失败**

测试两个 runner 竞争时只有一个成功 claim；过期 `sending` 可被重新领取；成功标记 `sent`；失败按 `1m、5m、15m、1h、6h` 回退；五次后标记 `failed`；单个坏事件不阻塞批次后续事件。

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./model ./service ./controller -run 'NotificationEventClaim|UsageNotificationDelivery' -count=1`

Expected: FAIL，提示 claim 或 handler 未定义。

- [ ] **Step 3: 实现 CAS 领取与状态转换**

先查询最多 50 条到期的 `pending` 或租约过期的 `sending`，再逐条执行条件更新：

```go
result := DB.Model(&UserNotificationEvent{}).
	Where("id = ? AND ((status = ? AND next_retry_at <= ?) OR (status = ? AND lease_until < ?))",
		id, NotificationStatusPending, now, NotificationStatusSending, now).
	Updates(map[string]any{
		"status": NotificationStatusSending, "locked_by": runnerID,
		"lease_until": now + 120, "updated_at": now,
	})
claimed := result.Error == nil && result.RowsAffected == 1
```

所有完成/失败更新都带 `id + status=用量提醒任务 + locked_by` 条件，防止租约丢失后的旧 runner 覆盖新结果。`LastError` 只保存去除密钥和个人信息的错误摘要，最长 1000 字节。

- [ ] **Step 4: 注册周期系统任务**

在 `model/system_task.go` 增加 `SystemTaskTypeUsageNotificationDelivery`。在 `controller/system_task_handlers.go` 注册实现 `ScheduledSystemTaskHandler` 的 handler：

```go
func (usageNotificationDeliveryHandler) Type() string {
	return model.SystemTaskTypeUsageNotificationDelivery
}
func (usageNotificationDeliveryHandler) Interval() time.Duration { return time.Minute }
func (usageNotificationDeliveryHandler) Enabled() bool           { return true }
func (usageNotificationDeliveryHandler) NewPayload() any         { return struct{}{} }
```

`Run` 用现有 `runnerID` 处理一批事件并调用 `finishSystemTaskHandler`。不要另起不受现有租约管理的常驻 goroutine。

- [ ] **Step 5: 增加投递可观测性**

在核心计划的 `usage_governance_metrics` 快照中增加 pending/sent/failed、投递延迟和重试次数；事件冲突记录 debug 级幂等信息，发送失败和最终失败记录结构化日志。日志只含用户 ID、事件 ID/type/key、attempt 和 request/runner ID。

- [ ] **Step 6: 运行测试并提交工作器切片**

Run: `go test ./model ./service ./controller -run 'SystemTask|NotificationEvent|UsageNotificationDelivery' -count=1`

Expected: PASS。

```bash
git add model/usage_notification.go model/usage_notification_test.go model/system_task.go service/usage_notification_delivery.go service/usage_notification_delivery_test.go controller/system_task_handlers.go controller/system_task_handlers_test.go
git commit -m "feat: deliver usage notification outbox"
```

---

### Task 5: 实现默认飞书、确定性邮箱回退和方案 B 卡片

**Files:**
- Modify: `relaykit/dto/notify.go`
- Modify: `service/user_notify.go`
- Create: `service/user_notify_test.go`
- Create: `service/usage_notification_card.go`
- Create: `service/usage_notification_card_test.go`

- [ ] **Step 1: 写失败测试锁定渠道决策**

通过包内可替换发送函数测试：空 `notify_type` 首选飞书；未绑定或系统未配置时回退账户邮箱；飞书请求已发出后的网络/API 错误只重试飞书；显式选择飞书的任何失败都不回退；显式邮件/Webhook/Bark/Gotify 保持原行为。

```go
func TestNotifyUserFallsBackToEmailOnlyForDefaultFeishuUnavailable(t *testing.T) {
	setting := dto.UserSetting{}
	err := notifyUserWithSenders(42, "user@example.com", setting, dto.Notify{}, notifySenderSet{
		feishu: func(int, dto.Notify) error { return ErrFeishuRecipientUnavailable },
		email:  func(to string, _ dto.Notify) error { require.Equal(t, "user@example.com", to); return nil },
	})
	require.NoError(t, err)
}
```

`notifyUserWithSenders` 是生产包内的依赖集合入口，`NotifyUser` 使用真实 sender 调用它；不要添加仅供测试的导出钩子。

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./service -run 'NotifyUser.*Feishu|UsageNotificationCard' -count=1`

Expected: FAIL，提示渠道决策或卡片构建器不存在。

- [ ] **Step 3: 区分发送前不可用与发送后失败**

定义稳定 sentinel：

```go
var (
	ErrFeishuAppUnavailable       = errors.New("feishu app unavailable")
	ErrFeishuRecipientUnavailable = errors.New("feishu recipient unavailable")
)
```

系统 AppID/AppSecret 缺失返回 `ErrFeishuAppUnavailable`；用户无 `FeishuId` 返回 `ErrFeishuRecipientUnavailable`，不能再返回 nil。只有用户 `notify_type == ""` 且 `errors.Is` 上述两类发送前错误时回退邮箱。获取 token、HTTP 超时和飞书 API 错误均保留为飞书失败，由 outbox 重试，避免一次事件可能发送两份。

- [ ] **Step 4: 构建方案 B 飞书 interactive 卡片**

在 `relaykit/dto/notify.go` 增加用量通知类型，并让 `Notify` 可携带可选结构化卡片内容，字段类型留在 relaykit 内：

```go
const (
	NotifyTypeDailyTokenMilestone = "daily_token_milestone"
	NotifyTypeSubscriptionUsage80 = "subscription_usage_80"
)

type Notify struct {
	Type        string        `json:"type"`
	Title       string        `json:"title"`
	Content     string        `json:"content"`
	Values      []interface{} `json:"values"`
	FeishuCard  any           `json:"feishu_card,omitempty"`
}
```

若不接受 `any` 作为跨模块 DTO，则在 relaykit 内定义只含 `Header`、`Elements` 的通用飞书卡片结构；不得反向 import 根模块。

每日卡片：标题“今日 Token 用量提醒”，副标题“已达到 {N}M 档位”，显示今日累计 Token、今日消耗人民币、下一档，并逐行展示所有有效订阅的套餐名、已用和剩余金额。订阅卡片：标题“订阅额度温馨提醒”，显示套餐名、已使用比例、剩余金额、本周期累计使用和周期结束时间。金额统一调用中文错误计划中的人民币格式化函数，日期使用北京时间。

为两个卡片增加 JSON 快照测试，断言 `msg_type=interactive`、中文标题、M 单位、人民币金额、多订阅不合并，并断言 payload 不含 `quota`、规则 ID、数据库字段或内部错误。

- [ ] **Step 5: 扩展飞书发送函数**

有卡片时把 `content` 用 `common.Marshal(data.FeishuCard)` 生成 JSON 字符串并发送 `msg_type=interactive`；其他现有通知继续发送 text。抽取共享 `sendFeishuMessage(tenantToken, openID, msgType, content string)`，保留超时、响应码校验和 `common.Unmarshal`。

- [ ] **Step 6: 独立验证 relaykit 并提交卡片切片**

Run: `cd relaykit && GOWORK=off go test ./dto -count=1 && GOWORK=off go build ./...`

Expected: PASS，且无根模块依赖。

Run: `go test ./service -run 'NotifyUser|Feishu|UsageNotificationCard' -count=1`

Expected: PASS。

```bash
git add relaykit/dto/notify.go service/user_notify.go service/user_notify_test.go service/usage_notification_card.go service/usage_notification_card_test.go
git commit -m "feat: send usage reminder cards through feishu"
```

---

### Task 6: 增加用户提醒开关并修正默认渠道界面

**Files:**
- Modify: `relaykit/dto/user_settings.go`
- Modify: `controller/user.go`
- Modify: `controller/user_test.go`
- Modify: `web/src/features/profile/types.ts`
- Modify: `web/src/features/profile/components/tabs/notification-tab.tsx`
- Create: `web/src/features/profile/components/tabs/__tests__/notification-tab.test.tsx`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/zh.json`
- Modify: `web/src/i18n/locales/fr.json`
- Modify: `web/src/i18n/locales/ru.json`
- Modify: `web/src/i18n/locales/ja.json`
- Modify: `web/src/i18n/locales/vi.json`
- Modify: `web/src/i18n/locales/zh-TW.json`

- [ ] **Step 1: 写失败测试覆盖未设置和显式关闭**

后端测试旧设置 JSON 没有字段时 `DailyTokenMilestoneNotifyEnabled()` 返回 true，显式 false 返回 false，更新其他设置不会把指针改成 false。前端测试空渠道显示飞书应用、每日提醒开关默认打开且能保存 false。

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./controller -run 'UsageNotificationSetting|DailyTokenMilestone' -count=1`

Run: `cd relaykit && GOWORK=off go test ./dto -run DailyTokenMilestone -count=1`

Expected: FAIL，提示字段或 helper 不存在。

Run: `cd web && bun test src/features/profile/components/tabs/__tests__/notification-tab.test.tsx`

Expected: FAIL，页面仍默认 email 或缺少开关。

- [ ] **Step 3: 增加可区分未设置的 DTO 字段**

```go
type UserSetting struct {
	// existing fields...
	DailyTokenMilestoneNotify *bool `json:"daily_token_milestone_notify,omitempty"`
}

func (s UserSetting) DailyTokenMilestoneNotifyEnabled() bool {
	return s.DailyTokenMilestoneNotify == nil || *s.DailyTokenMilestoneNotify
}
```

controller 更新请求也用 `*bool`，只在字段出现时覆盖，避免部分更新破坏历史默认值。该设置只控制每日 Token 里程碑；订阅 80% 安全提醒始终开启。

- [ ] **Step 4: 更新通知设置 UI 和翻译**

将 `notification-tab.tsx` 的初始 `notify_type` 从 `email` 改为 `feishu_app`，同时保留服务端空字符串表示“未显式设置”；界面归一化函数只负责展示，不应在用户未修改时自动写回显式飞书。新增每日 Token 用量提醒开关，说明文字只描述通知内容，不展示内部实现。

运行 i18n 同步并人工修正所有语言，不把中文原文直接复制到非中文 locale。

- [ ] **Step 5: 验证并提交设置切片**

Run: `cd relaykit && GOWORK=off go test ./dto -count=1 && GOWORK=off go build ./...`

Run: `go test ./controller -run 'UsageNotificationSetting|UpdateUserSetting' -count=1`

Run: `cd web && bun run i18n:sync && bun test src/features/profile/components/tabs/__tests__/notification-tab.test.tsx && bun run typecheck && bun run lint`

Expected: 全部 PASS，`bun run i18n:sync` 后无未提交的意外键删除。

```bash
git add relaykit/dto/user_settings.go controller/user.go controller/user_test.go web/src/features/profile/types.ts web/src/features/profile/components/tabs/notification-tab.tsx web/src/features/profile/components/tabs/__tests__/notification-tab.test.tsx web/src/i18n/locales
git commit -m "feat: add usage reminder preferences"
```

---

### Task 7: 完成端到端、并发和三数据库验证

**Files:**
- Create: `service/usage_notification_integration_test.go`
- Modify: `docs/superpowers/specs/2026-08-06-user-usage-limits-and-notifications-design.md`（仅在实现与已确认规格存在必要偏差时记录决策）

- [ ] **Step 1: 增加端到端集成测试**

覆盖：实际结算 -> 每日计数 -> 最高档事件 -> worker claim -> 飞书卡片发送 -> sent；订阅达到 80% -> 独立事件 -> 卡片；默认飞书未绑定 -> 邮箱；显式飞书失败 -> pending 重试。HTTP sender 使用 `httptest.Server` 或注入 sender，测试不得访问真实飞书或邮件服务。

- [ ] **Step 2: 运行竞态与跨包测试**

Run: `go test -race ./model ./service ./controller -run 'UsageNotification|DailyToken|Subscription80|NotifyUser' -count=1`

Expected: PASS，无数据竞争、重复事件或负计数。

- [ ] **Step 3: 验证三种数据库**

使用项目现有数据库测试环境分别运行：

```bash
go test ./model ./service -run 'UserDailyUsage|NotificationEvent|UsageNotification' -count=1
```

Expected: SQLite、MySQL >= 5.7.8、PostgreSQL >= 9.6 全部 PASS；`ON CONFLICT`/`ON DUPLICATE KEY` 由 GORM 方言生成，不包含手写方言 SQL。

- [ ] **Step 4: 运行完整质量门禁**

Run: `go test ./...`

Run: `go vet ./...`

Run: `cd relaykit && GOWORK=off go test ./... && GOWORK=off go build ./...`

Run: `cd web && bun run build:check`

Expected: 全部 PASS。

- [ ] **Step 5: 浏览器验证通知设置**

按项目要求构建并启动单一后端 origin，在桌面和移动视口检查通知设置：默认展示飞书、渠道切换配置正确、每日开关可操作、中文文本无截断或重叠。记录截图与控制台/网络错误检查结果；飞书卡片样式以 payload 快照和飞书测试租户实发二者验证，缺少测试租户凭据时明确记录未实发。

- [ ] **Step 6: 审查发布和回滚条件**

确认新增表和列只增不删；禁用工作器不会影响请求结算；回滚旧应用时 outbox 数据保留；pending/failed 指标可见；日志不含邮箱、飞书 open_id、Webhook 密钥或 API 密钥。

- [ ] **Step 7: 提交集成验证**

```bash
git add service/usage_notification_integration_test.go
git commit -m "test: cover usage notification workflow"
```
