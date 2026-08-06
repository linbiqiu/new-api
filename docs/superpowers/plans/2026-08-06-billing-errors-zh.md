# 中文额度错误 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将钱包、订阅和 API 令牌额度错误改为稳定错误码与始终中文的用户提示，同时移除基于英文错误字符串的分支。

**Architecture:** 模型层返回可用 `errors.Is` / `errors.As` 判断的类型化订阅错误；service 层集中把资金来源错误转换成 OpenAI 兼容的 `NewAPIError`。人民币格式由稳定的领域函数统一生成，原始错误只进入服务端日志。

**Tech Stack:** Go 1.22+、Gin、GORM v2、`relaykit/types`、`testify`

**Canonical spec:** `docs/superpowers/specs/2026-08-06-user-usage-limits-and-notifications-design.md`

**Execution order:** 本计划先执行；总量限制计划复用这里定义的错误码和格式化函数。

---

### Task 1: 扩充 relaykit 稳定错误码

**Files:**
- Modify: `relaykit/types/error.go`
- Test: `relaykit/types/error_test.go`

- [ ] **Step 1: 写失败测试，锁定 OpenAI 错误结构与中文消息**

在 `relaykit/types/error_test.go` 增加表驱动测试，至少覆盖：

```go
func TestQuotaErrorsKeepStableOpenAIErrorCode(t *testing.T) {
	apiErr := NewErrorWithStatusCode(
		errors.New("当前账户余额已使用完毕，请充值后继续使用。"),
		ErrorCodeWalletQuotaExhausted,
		http.StatusForbidden,
	)
	got := apiErr.ToOpenAIError()
	require.Equal(t, "当前账户余额已使用完毕，请充值后继续使用。", got.Message)
	require.Equal(t, ErrorCodeWalletQuotaExhausted, got.Code)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
}
```

- [ ] **Step 2: 运行测试并确认因常量不存在而失败**

Run: `cd relaykit && GOWORK=off go test ./types -run TestQuotaErrorsKeepStableOpenAIErrorCode -count=1`

Expected: FAIL，提示 `undefined: ErrorCodeWalletQuotaExhausted`。

- [ ] **Step 3: 在 `relaykit/types/error.go` 定义完整额度错误码**

在 quota error 常量组加入：

```go
ErrorCodeWalletQuotaExhausted             ErrorCode = "wallet_quota_exhausted"
ErrorCodeWalletQuotaInsufficient          ErrorCode = "wallet_quota_insufficient"
ErrorCodeSubscriptionUnavailable          ErrorCode = "subscription_unavailable"
ErrorCodeSubscriptionExpired              ErrorCode = "subscription_expired"
ErrorCodeSubscriptionPeriodExhausted      ErrorCode = "subscription_period_quota_exhausted"
ErrorCodeSubscriptionPeriodInsufficient   ErrorCode = "subscription_period_quota_insufficient"
ErrorCodeAPITokenQuotaExhausted           ErrorCode = "api_token_quota_exhausted"
ErrorCodeAPITokenQuotaInsufficient        ErrorCode = "api_token_quota_insufficient"
ErrorCodeAllModelsAmountLimitExhausted    ErrorCode = "all_models_amount_limit_exhausted"
ErrorCodeAllModelsAmountLimitInsufficient ErrorCode = "all_models_amount_limit_insufficient"
ErrorCodeAllModelsTokenLimitExhausted     ErrorCode = "all_models_token_limit_exhausted"
ErrorCodeModelAmountLimitExhausted        ErrorCode = "model_amount_limit_exhausted"
ErrorCodeModelAmountLimitInsufficient     ErrorCode = "model_amount_limit_insufficient"
ErrorCodeUsageLimitCheckUnavailable       ErrorCode = "usage_limit_check_unavailable"
```

保留现有 `ErrorCodeInsufficientUserQuota` 和 `ErrorCodePreConsumeTokenQuotaFailed`，避免旧调用方编译或行为回归；新代码不再使用它们表示上述细分场景。

- [ ] **Step 4: 运行 relaykit 测试和独立构建**

Run: `cd relaykit && GOWORK=off go test ./types -count=1`

Expected: PASS。

Run: `cd relaykit && GOWORK=off go build ./...`

Expected: exit 0，且 `relaykit/` 未引入根模块 import。

- [ ] **Step 5: 提交错误码契约**

```bash
git add relaykit/types/error.go relaykit/types/error_test.go
git commit -m "feat: add stable quota error codes"
```

### Task 2: 用类型化错误替代订阅英文字符串

**Files:**
- Modify: `model/subscription.go`
- Test: `model/subscription_auth_test.go`
- Test: `model/subscription_reset_test.go`

- [ ] **Step 1: 写失败测试，区分无订阅与额度不足**

增加两个行为测试：

```go
func TestPreConsumeSubscriptionReturnsTypedUnavailableError(t *testing.T) {
	_, err := PreConsumeUserSubscription("req-no-sub", 901, "gpt-5", 0, 10)
	require.ErrorIs(t, err, ErrNoActiveSubscription)
}

func TestPreConsumeSubscriptionReturnsTypedInsufficientDetails(t *testing.T) {
	// fixture: amount_total=100, amount_used=90, request amount=20
	_, err := PreConsumeUserSubscription("req-low-sub", userID, "gpt-5", 0, 20)
	var quotaErr *SubscriptionQuotaInsufficientError
	require.ErrorAs(t, err, &quotaErr)
	require.EqualValues(t, 10, quotaErr.Remaining)
	require.EqualValues(t, 20, quotaErr.Required)
}
```

- [ ] **Step 2: 运行测试并确认类型不存在**

Run: `go test ./model -run 'TestPreConsumeSubscriptionReturnsTyped(Unavailable|Insufficient)' -count=1`

Expected: FAIL，提示类型或哨兵错误未定义。

- [ ] **Step 3: 定义模型层错误并替换字符串返回**

在 `model/subscription.go` 定义：

```go
var ErrNoActiveSubscription = errors.New("no active subscription")

type SubscriptionQuotaInsufficientError struct {
	Remaining int64
	Required  int64
}

func (e *SubscriptionQuotaInsufficientError) Error() string {
	return fmt.Sprintf("subscription quota insufficient: remaining=%d required=%d", e.Remaining, e.Required)
}
```

在 `preConsumeUserSubscriptionInternal` 中：

- 没有 active subscription 时返回 `ErrNoActiveSubscription`。
- 遍历候选订阅时记录最大的非负剩余额度。
- 全部候选均不足时返回 `&SubscriptionQuotaInsufficientError{Remaining: maxRemaining, Required: amount}`。
- 不改变锁、预扣记录、重置和事务边界。

- [ ] **Step 4: 运行订阅模型测试**

Run: `go test ./model -run 'Subscription|PreConsume' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交类型化模型错误**

```bash
git add model/subscription.go model/subscription_auth_test.go model/subscription_reset_test.go
git commit -m "refactor: type subscription quota errors"
```

### Task 3: 建立人民币格式与额度 API 错误工厂

**Files:**
- Create: `service/quota_error.go`
- Test: `service/quota_error_test.go`

- [ ] **Step 1: 写失败测试，锁定人民币格式和永久周期文案**

```go
func TestFormatQuotaCNYAlwaysUsesRMB(t *testing.T) {
	original := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7
	t.Cleanup(func() { operation_setting.USDExchangeRate = original })
	require.Equal(t, "¥7.00", formatQuotaCNY(int64(common.QuotaFromFloat(1*common.QuotaPerUnit))))
}

func TestNewUsageLimitErrorOmitsResetForTotalPeriod(t *testing.T) {
	err := newAmountLimitError(amountLimitErrorInput{
		Scope: "all", PeriodLabel: "永久累计", Limit: int64(common.QuotaPerUnit), Permanent: true,
	})
	require.NotContains(t, err.Error(), "重置")
	require.Equal(t, types.ErrorCodeAllModelsAmountLimitExhausted, err.GetErrorCode())
}
```

- [ ] **Step 2: 运行测试并确认函数不存在**

Run: `go test ./service -run 'TestFormatQuotaCNY|TestNewUsageLimitError' -count=1`

Expected: FAIL。

- [ ] **Step 3: 实现集中错误工厂**

`service/quota_error.go` 只承担稳定领域格式与错误构造：

```go
func formatQuotaCNY(quota int64) string {
	cny := float64(quota) / common.QuotaPerUnit * operation_setting.USDExchangeRate
	return fmt.Sprintf("¥%.2f", cny)
}

func newQuotaAPIError(code types.ErrorCode, message string, status int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(message), code, status,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}
```

同文件实现钱包、订阅、API 令牌和后续总量限制复用的输入结构与构造函数。消息必须逐字遵循 canonical spec 第 9 节；永久周期使用“请联系管理员调整额度”，不拼接重置时间。

- [ ] **Step 4: 运行错误工厂测试**

Run: `go test ./service -run 'QuotaError|FormatQuotaCNY|UsageLimitError' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交错误工厂**

```bash
git add service/quota_error.go service/quota_error_test.go
git commit -m "feat: format quota errors in Chinese"
```

### Task 4: 接入钱包、订阅与 API 令牌预扣链路

**Files:**
- Modify: `service/billing_session.go`
- Modify: `service/quota.go`
- Test: `service/billing_session_test.go`
- Test: `service/text_quota_test.go`

- [ ] **Step 1: 写失败测试覆盖六个外部场景**

表驱动测试至少断言：钱包为 0、钱包不足本次预扣、无订阅、订阅剩余不足、API 令牌为 0、API 令牌不足。每个用例同时断言 HTTP 403、细分错误码、中文消息且不含 `quota`、`need=`、`subscription quota insufficient`。

```go
require.Equal(t, types.ErrorCodeWalletQuotaInsufficient, apiErr.GetErrorCode())
require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
require.Contains(t, apiErr.Error(), "当前账户余额不足以完成本次请求")
require.NotContains(t, apiErr.Error(), "need quota")
```

- [ ] **Step 2: 运行测试并确认仍返回旧错误码或英文**

Run: `go test ./service -run 'BillingSession.*Quota|TokenQuota.*Chinese' -count=1`

Expected: FAIL，实际仍为 `insufficient_user_quota`、`pre_consume_token_quota_failed` 或英文消息。

- [ ] **Step 3: 删除字符串匹配并映射类型化错误**

在 `BillingSession.preConsume` 和 `NewBillingSession`：

- 删除 `strings.Contains(errMsg, "no active subscription")` 分支。
- 使用 `errors.Is(err, model.ErrNoActiveSubscription)`。
- 使用 `errors.As(err, &model.SubscriptionQuotaInsufficientError{})`。
- 钱包分支根据 `userQuota <= 0` 与 `userQuota < preConsumedQuota` 返回不同错误码。
- 订阅过期检测仅在不存在 active subscription 时查询最近一条已到期订阅；查不到才返回 `subscription_unavailable`。
- 订阅禁止钱包回退时仍返回订阅额度错误，不暴露回退策略。

在 `PreConsumeTokenQuota`：

- `RemainQuota <= 0` 返回 `api_token_quota_exhausted`。
- `RemainQuota < quota` 返回 `api_token_quota_insufficient`。
- 不直接拼接 token 内部 quota 值。

- [ ] **Step 4: 运行 service 回归测试**

Run: `go test ./service -run 'Billing|Quota|Subscription' -count=1`

Expected: PASS。

- [ ] **Step 5: 运行相关 controller/relay 错误结构测试**

Run: `go test ./controller ./relay/... -run 'Quota|Billing|Error' -count=1`

Expected: PASS；若包中没有匹配测试，命令仍应 exit 0 并明确显示 `[no tests to run]`。

- [ ] **Step 6: 提交计费链路接入**

```bash
git add service/billing_session.go service/quota.go service/billing_session_test.go service/text_quota_test.go
git commit -m "fix: return friendly Chinese billing errors"
```

### Task 5: 完成本计划验证

**Files:**
- Verify only

- [ ] **Step 1: 格式化并检查 diff**

Run: `gofmt -w relaykit/types/error.go relaykit/types/error_test.go model/subscription.go service/quota_error.go service/quota_error_test.go service/billing_session.go service/quota.go`

Run: `git diff --check`

Expected: 无输出。

- [ ] **Step 2: 执行根模块相关测试和构建**

Run: `go test ./model ./service ./controller ./middleware -count=1`

Expected: PASS。

Run: `go build ./...`

Expected: exit 0。

- [ ] **Step 3: 再次验证 relaykit 独立构建**

Run: `cd relaykit && GOWORK=off go test ./... && GOWORK=off go build ./...`

Expected: PASS / exit 0。

- [ ] **Step 4: 确认提交范围**

Run: `git status --short`

Expected: 本计划修改均已提交；用户现有 IDOne、飞书认证等无关工作区修改保持原样。
