# 用户总量限制核心 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有分组、用户和订阅计划模型额度规则中增加按用户独立核算的全部模型总金额与总 Token 上限，并提供兼容的管理端配置和审计能力。

**Architecture:** 扩展现有三类规则与 `user_model_quota_usage`，用结构化检查结果替代英文字符串。分组和用户规则在中间件预检，订阅计划规则绑定实际选中的计费订阅；结算入口同步累计实际金额和 Token，Redis 仅作缓存且失败时回退数据库。

**Tech Stack:** Go 1.22+、Gin、GORM v2、Redis、React 19、TypeScript、TanStack Query、Base UI、Bun

**Canonical spec:** `docs/superpowers/specs/2026-08-06-user-usage-limits-and-notifications-design.md`

**Prerequisite:** 完成 `docs/superpowers/plans/2026-08-06-billing-errors-zh.md`。

---

### Task 1: 扩展规则和使用记录模型

**Files:**
- Modify: `model/model_quota.go`
- Modify: `model/main.go`
- Test: `model/model_quota_test.go`

- [ ] **Step 1: 写失败测试覆盖 schema 默认值、唯一身份和非负增量**

增加测试：历史规则空 `scope` 归一化为 `model`；全部模型规则能保存 `token_limit`；相同用户/规则/订阅/周期不能创建两条 active 记录；金额或 Token 负增量被拒绝；重置同时清零两项。

```go
func createModelQuotaUsageFixture(t *testing.T, quotaUsed, tokenUsed int64) UserModelQuotaUsage {
	t.Helper()
	usage := UserModelQuotaUsage{
		UserId: 1, RuleId: 1, RuleSource: ModelQuotaRuleSourceUser,
		ModelPattern: "*", SubscriptionId: 0,
		QuotaLimit: 1000, QuotaUsed: quotaUsed,
		TokenLimit: 1000, TokenUsed: tokenUsed,
		PeriodStart: 1, PeriodEnd: 4_102_444_800,
		Status: ModelQuotaUsageStatusActive,
	}
	require.NoError(t, DB.Create(&usage).Error)
	return usage
}

func TestIncreaseUserModelQuotaUsageRejectsNegativeDelta(t *testing.T) {
	usage := createModelQuotaUsageFixture(t, 100, 200)
	err := IncreaseUserModelQuotaUsage(usage.Id, -1, 0)
	require.ErrorContains(t, err, "delta must be non-negative")
}

func TestResetUserModelQuotaUsageClearsAmountAndTokens(t *testing.T) {
	usage := createModelQuotaUsageFixture(t, 100, 200)
	require.NoError(t, ResetUserModelQuotaUsage(usage.Id))
	require.NoError(t, DB.First(&usage, usage.Id).Error)
	require.Zero(t, usage.QuotaUsed)
	require.Zero(t, usage.TokenUsed)
}
```

将现有 `TestIncreaseUserModelQuotaUsage_Negative` 从“允许减额度”改为上述拒绝语义。

- [ ] **Step 2: 运行模型测试并确认失败**

Run: `go test ./model -run 'ModelQuota|UserModelQuotaUsage' -count=1`

Expected: FAIL，缺少字段或新函数签名。

- [ ] **Step 3: 增加字段、归一化与唯一索引**

在 `model/model_quota.go` 定义：

```go
const (
	ModelQuotaScopeModel = "model"
	ModelQuotaScopeAll   = "all"
)

type UserModelQuotaUsage struct {
	// existing fields...
	TokenLimit int64 `json:"token_limit" gorm:"column:token_limit;type:bigint;not null;default:0"`
	TokenUsed  int64 `json:"token_used" gorm:"column:token_used;type:bigint;not null;default:0"`
}
```

三张规则表均增加 `Scope string` 与 `TokenLimit int64`。`BeforeCreate` 将空 `Scope` 归一化为 `model`。使用记录增加由 `user_id`、`rule_id`、`rule_source`、`subscription_id`、`period_start`、`period_end` 组成的唯一索引；索引名和 priority 在所有字段 tag 中保持一致。

- [ ] **Step 4: 实现同步双指标增量与重置**

替换单一 delta API：

```go
func IncreaseUserModelQuotaUsage(usageID int, quotaDelta, tokenDelta int64) error {
	if quotaDelta < 0 || tokenDelta < 0 {
		return fmt.Errorf("usage delta must be non-negative: quota=%d tokens=%d", quotaDelta, tokenDelta)
	}
	return DB.Model(&UserModelQuotaUsage{}).Where("id = ?", usageID).Updates(map[string]any{
		"quota_used": gorm.Expr("quota_used + ?", quotaDelta),
		"token_used": gorm.Expr("token_used + ?", tokenDelta),
		"updated_at": common.GetTimestamp(),
	}).Error
}
```

计数器更新必须同步落库；不能继续把唯一持久化写入放在 `gopool.Go`。保留 batch update 仅在能同时携带两个 `int64` delta 且不会转换为 `int` 时使用，否则对模型额度计数器禁用 batch 路径。

- [ ] **Step 5: 注册迁移并初始化历史 scope**

在 `model/main.go` 现有四个模型额度实体旁保留 AutoMigrate 注册，并在 AutoMigrate 后增加幂等迁移：

```go
func initializeModelQuotaRuleScopes() error {
	for _, table := range []any{&ModelQuotaGroupRule{}, &ModelQuotaPlanRule{}, &ModelQuotaUserRule{}} {
		if err := DB.Model(table).Where("scope = ? OR scope IS NULL", "").Update("scope", ModelQuotaScopeModel).Error; err != nil {
			return err
		}
	}
	return nil
}
```

不得修改或覆盖 `model/main.go` 中用户尚未提交的 IDOne 迁移内容。

- [ ] **Step 6: 运行模型测试**

Run: `go test ./model -run 'ModelQuota|UserModelQuotaUsage' -count=1`

Expected: PASS。

- [ ] **Step 7: 提交模型切片**

```bash
git add model/model_quota.go model/model_quota_test.go model/main.go
git commit -m "feat: store aggregate usage limits"
```

### Task 2: 将 Redis 缓存扩展为双指标快照

**Files:**
- Modify: `model/model_quota_cache.go`
- Test: `model/model_quota_cache_test.go`

- [ ] **Step 1: 写失败测试覆盖四字段快照和 Redis 故障回退信号**

```go
func TestModelQuotaCacheRoundTripsAmountAndTokens(t *testing.T) {
	want := ModelQuotaUsageSnapshot{QuotaUsed: 12, QuotaLimit: 100, TokenUsed: 34, TokenLimit: 200}
	require.NoError(t, CacheSetModelQuotaUsage(7, want, time.Now().Add(time.Hour).Unix()))
	got, ok, err := CacheGetModelQuotaUsage(7)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, want, got)
}
```

- [ ] **Step 2: 运行测试并确认旧 API 不匹配**

Run: `go test ./model -run ModelQuotaCache -count=1`

Expected: FAIL。

- [ ] **Step 3: 实现结构化缓存 API**

```go
type ModelQuotaUsageSnapshot struct {
	QuotaUsed  int64
	QuotaLimit int64
	TokenUsed  int64
	TokenLimit int64
}

func CacheGetModelQuotaUsage(usageID int) (ModelQuotaUsageSnapshot, bool, error)
func CacheSetModelQuotaUsage(usageID int, snapshot ModelQuotaUsageSnapshot, periodEnd int64) error
func CacheIncrModelQuotaUsage(usageID int, quotaDelta, tokenDelta int64) error
```

缓存函数返回真实错误，不能把 Redis 故障折叠成普通 miss。service 层只有在 `common.RedisEnabled` 且缓存错误时回退数据库并记录指标；DB 读取成功后回填缓存失败不阻止当前请求。

- [ ] **Step 4: 运行缓存测试和 model 测试**

Run: `go test ./model -run 'ModelQuotaCache|ModelQuotaUsage' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交缓存切片**

```bash
git add model/model_quota_cache.go model/model_quota_cache_test.go
git commit -m "feat: cache amount and token limits"
```

### Task 3: 实现全部模型匹配、北京时间周期和 fail-closed 检查

**Files:**
- Modify: `service/model_quota.go`
- Test: `service/model_quota_test.go`
- Modify: `middleware/model_quota_limit.go`
- Test: `middleware/model_quota_limit_test.go`

- [ ] **Step 1: 写失败测试覆盖规则匹配和实用 Token 上限**

至少新增：

```go
func createGroupAllRule(t *testing.T, group, period string, quotaLimit, tokenLimit int64) model.ModelQuotaGroupRule {
	t.Helper()
	rule := model.ModelQuotaGroupRule{
		GroupName: group, Scope: model.ModelQuotaScopeAll,
		Period: period, QuotaLimit: quotaLimit, TokenLimit: tokenLimit,
		Enabled: true,
	}
	require.NoError(t, model.DB.Create(&rule).Error)
	return rule
}

func createUsage(t *testing.T, ruleID int, tokenUsed int64) model.UserModelQuotaUsage {
	t.Helper()
	usage := model.UserModelQuotaUsage{
		UserId: 101, RuleId: ruleID, RuleSource: model.ModelQuotaRuleSourceGroup,
		ModelPattern: "*", TokenLimit: 100, TokenUsed: tokenUsed,
		PeriodStart: 1, PeriodEnd: 4_102_444_800,
		Status: model.ModelQuotaUsageStatusActive,
	}
	require.NoError(t, model.DB.Create(&usage).Error)
	return usage
}

func TestCheckModelQuotaAllScopeMatchesEveryModel(t *testing.T) {
	createGroupAllRule(t, "default", model.ModelQuotaPeriodMonthly, 0, 100_000_000)
	result, err := CheckPreFundingModelQuota(101, "claude-opus", "default", 1)
	require.NoError(t, err)
	require.True(t, result.Passed)
	require.NotEmpty(t, result.UsageIDs)
}

func TestCheckModelQuotaBlocksWhenTokenLimitAlreadyReached(t *testing.T) {
	rule := createGroupAllRule(t, "default", model.ModelQuotaPeriodDaily, 0, 100)
	createUsage(t, rule.Id, 100)
	result, err := CheckPreFundingModelQuota(101, "gpt-5", "default", 1)
	require.NoError(t, err)
	require.False(t, result.Passed)
	require.Equal(t, types.ErrorCodeAllModelsTokenLimitExhausted, result.APIError.GetErrorCode())
}
```

另加北京时间日/周/月边界、任一规则取交集、规则查询失败返回 error 的测试。

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./service ./middleware -run 'ModelQuota|UsageLimit' -count=1`

Expected: FAIL。

- [ ] **Step 3: 将检查结果改为结构化错误**

```go
type ModelQuotaCheckResult struct {
	Passed   bool
	UsageIDs []int
	APIError *types.NewAPIError
}
```

实现 `CheckPreFundingModelQuota`：

- 只解析用户和分组规则；计划规则留给 Task 4。
- `scope=all` 无条件匹配模型。
- `scope=model` 继续 exact/prefix 匹配。
- 金额检查使用 `used + preQuota > limit`，区分 exhausted 与 insufficient。
- Token 检查只用 `token_used >= token_limit`。
- 任一 DB/缓存读取失败向上返回 error，不再 `continue`。
- 周期计算显式加载 `time.LoadLocation("Asia/Shanghai")`；加载失败应在进程初始化或测试中暴露，不能静默回退服务器时区。

- [ ] **Step 4: 中间件改为 fail-closed**

`middleware/model_quota_limit.go` 在 service error 时返回 HTTP 503 和 `usage_limit_check_unavailable`；在业务拒绝时使用 `result.APIError` 的状态和 OpenAI 结构。保留 `UsingGroup` 优先于 `UserGroup`。

- [ ] **Step 5: 运行 service 与 middleware 测试**

Run: `go test ./service ./middleware -run 'ModelQuota|UsageLimit' -count=1`

Expected: PASS。

- [ ] **Step 6: 提交检查切片**

```bash
git add service/model_quota.go service/model_quota_test.go middleware/model_quota_limit.go middleware/model_quota_limit_test.go
git commit -m "feat: enforce aggregate amount and token limits"
```

### Task 4: 将套餐规则绑定实际计费订阅

**Files:**
- Modify: `service/funding_source.go`
- Modify: `service/billing_session.go`
- Modify: `service/model_quota.go`
- Test: `service/group_subscription_test.go`
- Test: `service/billing_session_test.go`

- [ ] **Step 1: 写失败测试覆盖多订阅和钱包回退**

创建两个有效订阅，令第一个额度不足、第二个成功预扣；断言只检查第二个订阅 plan 规则。再创建允许钱包回退的场景，断言最终钱包计费不命中任何计划规则。

```go
require.Equal(t, fundedSubscriptionID, session.relayInfo.SubscriptionId)
rawUsageIDs, exists := c.Get(middleware.ModelQuotaLimitKey)
require.True(t, exists)
usageIDs, ok := rawUsageIDs.([]int)
require.True(t, ok)
require.ElementsMatch(t, []int{fundedPlanUsageID}, usageIDs)
```

- [ ] **Step 2: 运行测试并确认当前实现错误选择第一条订阅**

Run: `go test ./service -run 'ActualSubscription|WalletFallback.*PlanRule' -count=1`

Expected: FAIL。

- [ ] **Step 3: 暴露资金来源选择结果并检查计划规则**

在 `SubscriptionFunding.PreConsume` 成功后已经保存的 `subscriptionId`、`PlanId`、周期和额度字段基础上，增加 `PeriodStart`、`PeriodEnd`。周期使用订阅 `LastResetTime` / `NextResetTime`；无重置计划回退 `StartTime` / `EndTime`。

在 `BillingSession.preConsume` 完成资金与 token 预扣、调用上游之前：

```go
if sub, ok := s.funding.(*SubscriptionFunding); ok {
	result, err := CheckFundedSubscriptionModelQuota(c, s.relayInfo, sub)
	if err != nil || !result.Passed {
		s.rollbackPreConsume()
		return usageLimitPreConsumeError(err, result)
	}
	appendModelQuotaUsageIDs(c, result.UsageIDs)
}
```

`rollbackPreConsume` 必须复用现有 token 与 funding 回滚路径，不能复制扣费逻辑。

- [ ] **Step 4: 运行多订阅和计费回滚测试**

Run: `go test ./service -run 'Subscription|BillingSession|PlanRule' -count=1`

Expected: PASS，且拒绝请求后订阅 `amount_used`、token `remain_quota` 均恢复。

- [ ] **Step 5: 提交实际订阅绑定**

```bash
git add service/funding_source.go service/billing_session.go service/model_quota.go service/group_subscription_test.go service/billing_session_test.go
git commit -m "fix: bind plan limits to funded subscription"
```

### Task 5: 结算时同步累计实际金额和 Token

**Files:**
- Modify: `service/billing.go`
- Modify: `service/model_quota_hook.go`
- Modify: `service/text_quota.go`
- Modify: `service/quota.go`
- Modify: `controller/relay.go`
- Modify: `service/task_billing.go`
- Modify: `model/task.go`
- Test: `service/model_quota_test.go`
- Test: `service/task_billing_test.go`

- [ ] **Step 1: 写失败测试覆盖文本、音频、无 Token 任务与退款**

测试统一观察输入：

```go
func TestRecordModelQuotaUsageStoresActualAmountAndTokens(t *testing.T) {
	RecordModelQuotaUsage([]int{usageID}, 321, 12_345)
	got := loadUsage(t, usageID)
	require.EqualValues(t, 321, got.QuotaUsed)
	require.EqualValues(t, 12_345, got.TokenUsed)
}
```

任务测试断言提交时只累计金额、任务失败退款只撤销该任务曾记录的贡献且不会降到 0 以下。把 usage IDs、原始贡献和原始北京时间日期存入 `Task.PrivateData`，并依赖现有 task CAS 保证只退款一次。

- [ ] **Step 2: 运行测试并确认旧 hook 只接受金额**

Run: `go test ./service -run 'ModelQuotaUsage|Task.*UsageContribution' -count=1`

Expected: FAIL。

- [ ] **Step 3: 扩展结算入口**

将签名改为：

```go
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int, actualTokens int64) error
```

只有 `BillingSession.Settle` 或旧结算路径成功后才调用同步观察：

```go
if err := recordModelQuotaFromContext(ctx, actualQuota, actualTokens); err != nil {
	logger.LogError(ctx, "record usage limit counters failed: "+err.Error())
}
```

调用方传值：

- `PostTextConsumeQuota`：`int64(summary.PromptTokens + summary.CompletionTokens)`。
- `PostAudioConsumeQuota`：`int64(usage.PromptTokens + usage.CompletionTokens)`。
- 图片 handler 经 text usage 有真实 Token 时照常传；无 Token 时传 0。
- 任务提交：传 0 Token。
- 其他固定价格结算路径：传 0 Token。

- [ ] **Step 4: 实现任务贡献的受约束撤销**

不得重新允许公共计数器负增量。新增仅供任务退款使用的 `ReverseTaskUsageContribution(taskID string)`：读取任务保存的 usage IDs 和原始 quota contribution，在任务 CAS 成功的同一业务分支中逐条执行 `quota_used = max(0, quota_used - contribution)` 的跨库兼容事务逻辑。SQLite/MySQL/PostgreSQL 不共享 `GREATEST` 语法保证时，使用 `lockForUpdate(tx)` 读取后由 Go 计算并 `UpdateColumn`。

- [ ] **Step 5: 运行结算和任务测试**

Run: `go test ./service ./controller -run 'Billing|Quota|Task' -count=1`

Expected: PASS。

- [ ] **Step 6: 提交结算观察切片**

```bash
git add service/billing.go service/model_quota_hook.go service/text_quota.go service/quota.go controller/relay.go service/task_billing.go model/task.go service/model_quota_test.go service/task_billing_test.go
git commit -m "feat: record settled amount and token usage"
```

### Task 6: 强化管理 API、规则生命周期和审计

**Files:**
- Modify: `controller/model_quota.go`
- Test: `controller/model_quota_test.go`
- Modify: `router/api-router.go`
- Create: `service/usage_governance_metrics.go`

- [ ] **Step 1: 写失败 controller 测试**

覆盖：全部模型至少一个上限；指定模型仍要求 model + 正金额；只改上限保留 used；改 scope/对象/周期归档旧 usage；删除规则保留历史 usage；重置双指标；操作审计包含 old/new；metrics 端点仅管理员可访问。

- [ ] **Step 2: 运行 controller 测试并确认失败**

Run: `go test ./controller -run 'ModelQuota|UsageGovernance' -count=1`

Expected: FAIL。

- [ ] **Step 3: 用请求 DTO 替代直接绑定 GORM model**

在 `controller/model_quota.go` 定义共享字段 DTO：

```go
type modelQuotaRuleInput struct {
	Scope        string `json:"scope"`
	ModelPattern string `json:"model_pattern"`
	MatchMode    string `json:"match_mode"`
	Period       string `json:"period"`
	QuotaLimit   int64  `json:"quota_limit"`
	TokenLimit   int64  `json:"token_limit"`
	Enabled      *bool  `json:"enabled"`
	SortOrder    int    `json:"sort_order"`
}
```

三个对象 DTO 再增加 `group_name`、`plan_id` 或 `user_id`。创建和更新统一调用验证函数；更新先读取旧规则，用字段差异判断“同步限额”或“归档旧 usage”。保留历史 API 路径和空 scope 兼容。

- [ ] **Step 4: 记录管理员操作审计**

每个 mutation 成功后调用 `model.RecordOperationAuditLog`，action 使用：

- `usage_limit.rule.create`
- `usage_limit.rule.update`
- `usage_limit.rule.enable`
- `usage_limit.rule.delete`
- `usage_limit.usage.reset`

params 中包含 rule source/id、target、旧值和新值，不包含 API key 或通知密钥。

- [ ] **Step 5: 暴露最小管理指标快照**

创建 `service/usage_governance_metrics.go`，用 `atomic.Uint64` 记录拦截、超限、Redis 回退、503 和计数器错误；提供 `SnapshotUsageGovernanceMetrics()`。注册 `GET /api/model-quota/metrics`，返回只读快照，不引入新依赖。

- [ ] **Step 6: 运行 controller/router 测试**

Run: `go test ./controller ./router -run 'ModelQuota|UsageGovernance' -count=1`

Expected: PASS。

- [ ] **Step 7: 提交管理 API 切片**

```bash
git add controller/model_quota.go controller/model_quota_test.go router/api-router.go service/usage_governance_metrics.go
git commit -m "feat: manage aggregate usage rules"
```

### Task 7: 更新管理前端与用户用量详情

**Files:**
- Modify: `web/src/features/model-quota/types.ts`
- Modify: `web/src/features/model-quota/api.ts`
- Modify: `web/src/features/model-quota/model-quota-rules-page.tsx`
- Modify: `web/src/features/model-quota/user-model-quota-dialog.tsx`
- Create: `web/src/features/model-quota/lib/token-units.ts`
- Test: `web/src/features/model-quota/lib/__tests__/token-units.test.ts`
- Modify: `web/src/hooks/use-sidebar-data.ts`
- Modify: `web/src/i18n/static-keys.ts`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/zh.json`
- Modify: `web/src/i18n/locales/fr.json`
- Modify: `web/src/i18n/locales/ru.json`
- Modify: `web/src/i18n/locales/ja.json`
- Modify: `web/src/i18n/locales/vi.json`
- Modify: `web/src/i18n/locales/zh-TW.json`

- [ ] **Step 1: 写 `M Token` 转换失败测试**

```ts
import { describe, expect, test } from 'bun:test'
import { parseMillionsToTokens, formatTokensAsMillions } from '../token-units'

describe('model quota token units', () => {
  test('converts at most three decimal places without floating drift', () => {
    expect(parseMillionsToTokens('1.25')).toBe(1_250_000)
    expect(parseMillionsToTokens('0.001')).toBe(1_000)
    expect(parseMillionsToTokens('1.0001')).toBeNull()
    expect(formatTokensAsMillions(100_000_000)).toBe('100')
  })
})
```

- [ ] **Step 2: 运行测试并确认模块不存在**

Run: `cd web && bun test src/features/model-quota/lib/__tests__/token-units.test.ts`

Expected: FAIL，module not found。

- [ ] **Step 3: 实现整数转换并扩展 Zod/API 类型**

`parseMillionsToTokens` 使用字符串拆分整数和小数部分，右侧补齐 6 位后转整数，拒绝负数、指数形式、超过三位小数和超过 JS safe integer 的值；不得用 `Number(value) * 1_000_000` 直接产生舍入误差。

类型增加 `scope: 'model' | 'all'`、`token_limit`、`token_used`、`token_remain` 和两个百分比字段。

- [ ] **Step 4: 更新规则表单和列表**

按已确认 mockup：

- 用分段控件选择全部模型/指定模型。
- 全部模型隐藏 model/match 字段。
- 金额 CNY 与 Token M 双输入至少一项。
- 指定模型保留现有金额规则，不显示 Token 输入。
- 列表显示范围、金额上限、Token 上限；未配置显示 `—`。
- 不改变 `/model-quota-rules` 路由。
- 标题和侧边栏改为“用量限制规则”。

- [ ] **Step 5: 更新使用详情**

每条 usage 同时显示金额与 Token 进度；某指标未配置时不渲染空进度。使用固定 grid track 和 `min-w-0`，保证移动端长模型名不覆盖金额。

- [ ] **Step 6: 同步 i18n**

所有新增可见文案使用 `t('English source key')`；运行 `bun run i18n:sync`，检查所有语言文件只增加对应键，不覆盖用户现有翻译修改。

- [ ] **Step 7: 运行前端测试、类型、lint 和构建**

Run: `cd web && bun test src/features/model-quota/lib/__tests__/token-units.test.ts`

Run: `cd web && bun run typecheck`

Run: `cd web && bun run lint`

Run: `cd web && bun run build:check`

Expected: 全部 PASS / exit 0。

- [ ] **Step 8: 提交前端切片**

```bash
git add web/src/features/model-quota web/src/hooks/use-sidebar-data.ts web/src/i18n/static-keys.ts web/src/i18n/locales
git commit -m "feat: configure aggregate usage limits"
```

### Task 8: 集成与跨库验证

**Files:**
- Verify only

- [ ] **Step 1: 执行后端相关测试和构建**

Run: `go test ./model ./service ./middleware ./controller ./router -count=1`

Run: `go build ./...`

Expected: PASS / exit 0。

- [ ] **Step 2: 执行 relaykit 独立构建**

Run: `cd relaykit && GOWORK=off go build ./...`

Expected: exit 0。

- [ ] **Step 3: 验证三种数据库**

使用项目现有数据库测试环境分别启动 SQLite、MySQL 5.7.8+、PostgreSQL 9.6+，执行迁移并运行 model quota 集成用例。每种数据库确认：新增列、唯一索引、并发懒创建、原子双字段增量、归档与重置。

Expected: 三种数据库全部通过；缺少环境时不得宣告发布就绪，必须记录未运行项并交由 CI 补齐。

- [ ] **Step 4: 浏览器回归**

启动 `cd web && bun run dev`，用 Playwright 或浏览器检查桌面和移动视口：三个 Tab、全部模型表单、金额/Token 双输入、列表、用户详情、禁用/错误状态无重叠。

- [ ] **Step 5: 最终 diff 和工作区检查**

Run: `git diff --check`

Run: `git status --short`

Expected: 本计划文件均已提交；用户原有无关修改未被覆盖或暂存。
