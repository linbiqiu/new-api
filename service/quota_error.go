package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func formatQuotaCNY(quota int64) string {
	if quota < 0 {
		quota = 0
	}
	cny := float64(quota) / common.QuotaPerUnit * operation_setting.USDExchangeRate
	return fmt.Sprintf("¥%.2f", cny)
}

func formatTokenMillions(tokens int64) string {
	if tokens < 0 {
		tokens = 0
	}
	whole := tokens / 1_000_000
	fraction := tokens % 1_000_000
	if fraction == 0 {
		return fmt.Sprintf("%d", whole)
	}
	value := fmt.Sprintf("%d.%06d", whole, fraction)
	return strings.TrimRight(value, "0")
}

func newQuotaAPIError(code types.ErrorCode, message string, status int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(message), code, status,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func newWalletQuotaError(remaining, required int64) *types.NewAPIError {
	if remaining <= 0 {
		return newQuotaAPIError(
			types.ErrorCodeWalletQuotaExhausted,
			"当前账户余额已使用完毕，请充值后继续使用。",
			http.StatusForbidden,
		)
	}
	return newQuotaAPIError(
		types.ErrorCodeWalletQuotaInsufficient,
		fmt.Sprintf("当前账户余额不足以完成本次请求。当前余额 %s，本次预计需要 %s。", formatQuotaCNY(remaining), formatQuotaCNY(required)),
		http.StatusForbidden,
	)
}

type subscriptionQuotaErrorInput struct {
	Plan      string
	Remaining int64
	Required  int64
	ResetAt   string
}

func newSubscriptionUnavailableError() *types.NewAPIError {
	return newQuotaAPIError(
		types.ErrorCodeSubscriptionUnavailable,
		"当前没有可用的订阅计划，请先开通或续费后再使用。",
		http.StatusForbidden,
	)
}

func newSubscriptionExpiredError(plan string) *types.NewAPIError {
	return newQuotaAPIError(
		types.ErrorCodeSubscriptionExpired,
		fmt.Sprintf("您的“%s”已到期，请续费后继续使用。", plan),
		http.StatusForbidden,
	)
}

func newSubscriptionQuotaError(input subscriptionQuotaErrorInput) *types.NewAPIError {
	if input.Remaining <= 0 {
		return newQuotaAPIError(
			types.ErrorCodeSubscriptionPeriodExhausted,
			fmt.Sprintf("您的“%s”本周期额度已使用完毕，将于 %s 重置。", input.Plan, input.ResetAt),
			http.StatusForbidden,
		)
	}
	return newQuotaAPIError(
		types.ErrorCodeSubscriptionPeriodInsufficient,
		fmt.Sprintf("您的“%s”本周期剩余额度不足以完成本次请求。剩余 %s，预计需要 %s，将于 %s 重置。", input.Plan, formatQuotaCNY(input.Remaining), formatQuotaCNY(input.Required), input.ResetAt),
		http.StatusForbidden,
	)
}

func newAPITokenQuotaError(exhausted bool) *types.NewAPIError {
	if exhausted {
		return newQuotaAPIError(
			types.ErrorCodeAPITokenQuotaExhausted,
			"当前 API 令牌额度已使用完毕，请更换令牌或联系管理员。",
			http.StatusForbidden,
		)
	}
	return newQuotaAPIError(
		types.ErrorCodeAPITokenQuotaInsufficient,
		"当前 API 令牌剩余额度不足以完成本次请求，请更换令牌或联系管理员。",
		http.StatusForbidden,
	)
}

type amountLimitErrorInput struct {
	Scope       string
	PeriodLabel string
	Model       string
	Limit       int64
	Remaining   int64
	Required    int64
	ResetAt     string
	Permanent   bool
}

func newAmountLimitError(input amountLimitErrorInput) *types.NewAPIError {
	suffix := fmt.Sprintf("，将于 %s 重置。", input.ResetAt)
	if input.Permanent {
		suffix = "，请联系管理员调整额度。"
	}

	if input.Scope == "all" {
		if input.Required > 0 && input.Remaining > 0 {
			message := fmt.Sprintf("您%s的全部模型金额剩余额度不足以完成本次请求。剩余 %s，预计需要 %s%s", input.PeriodLabel, formatQuotaCNY(input.Remaining), formatQuotaCNY(input.Required), suffix)
			return newQuotaAPIError(types.ErrorCodeAllModelsAmountLimitInsufficient, message, http.StatusForbidden)
		}
		message := fmt.Sprintf("您%s的全部模型金额额度已达到上限（%s）%s", input.PeriodLabel, formatQuotaCNY(input.Limit), suffix)
		return newQuotaAPIError(types.ErrorCodeAllModelsAmountLimitExhausted, message, http.StatusForbidden)
	}

	if input.Required > 0 && input.Remaining > 0 {
		message := fmt.Sprintf("您%s的“%s”模型金额剩余额度不足以完成本次请求。剩余 %s，预计需要 %s%s", input.PeriodLabel, input.Model, formatQuotaCNY(input.Remaining), formatQuotaCNY(input.Required), suffix)
		return newQuotaAPIError(types.ErrorCodeModelAmountLimitInsufficient, message, http.StatusForbidden)
	}
	message := fmt.Sprintf("您%s的“%s”模型金额额度已达到上限（%s）%s", input.PeriodLabel, input.Model, formatQuotaCNY(input.Limit), suffix)
	return newQuotaAPIError(types.ErrorCodeModelAmountLimitExhausted, message, http.StatusForbidden)
}

type tokenLimitErrorInput struct {
	PeriodLabel string
	Limit       int64
	ResetAt     string
	Permanent   bool
}

func newTokenLimitError(input tokenLimitErrorInput) *types.NewAPIError {
	suffix := fmt.Sprintf("，将于 %s 重置。", input.ResetAt)
	if input.Permanent {
		suffix = "，请联系管理员调整额度。"
	}
	message := fmt.Sprintf("您%s的全部模型 Token 用量已达到上限（%sM）%s", input.PeriodLabel, formatTokenMillions(input.Limit), suffix)
	return newQuotaAPIError(types.ErrorCodeAllModelsTokenLimitExhausted, message, http.StatusForbidden)
}

func newUsageLimitCheckUnavailableError() *types.NewAPIError {
	return newQuotaAPIError(
		types.ErrorCodeUsageLimitCheckUnavailable,
		"用量校验暂时不可用，请稍后重试。",
		http.StatusServiceUnavailable,
	)
}
