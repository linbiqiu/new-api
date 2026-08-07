package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestFormatQuotaCNYAlwaysUsesRMB(t *testing.T) {
	original := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7
	t.Cleanup(func() { operation_setting.USDExchangeRate = original })

	require.Equal(t, "¥7.00", formatQuotaCNY(int64(common.QuotaPerUnit)))
}

func TestNewUsageLimitErrorOmitsResetForTotalPeriod(t *testing.T) {
	err := newAmountLimitError(amountLimitErrorInput{
		Scope: "all", PeriodLabel: "永久累计", Limit: int64(common.QuotaPerUnit), Permanent: true,
	})

	require.NotContains(t, err.Error(), "重置")
	require.Contains(t, err.Error(), "请联系管理员调整额度")
	require.Equal(t, types.ErrorCodeAllModelsAmountLimitExhausted, err.GetErrorCode())
}

func TestQuotaErrorCatalog(t *testing.T) {
	original := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7
	t.Cleanup(func() { operation_setting.USDExchangeRate = original })

	tests := []struct {
		name    string
		err     *types.NewAPIError
		code    types.ErrorCode
		message string
		status  int
	}{
		{"wallet exhausted", newWalletQuotaError(0, 10), types.ErrorCodeWalletQuotaExhausted, "当前账户余额已使用完毕，请充值后继续使用。", http.StatusForbidden},
		{"wallet insufficient", newWalletQuotaError(int64(common.QuotaPerUnit), int64(common.QuotaPerUnit*2)), types.ErrorCodeWalletQuotaInsufficient, "当前账户余额不足以完成本次请求。当前余额 ¥7.00，本次预计需要 ¥14.00。", http.StatusForbidden},
		{"subscription unavailable", newSubscriptionUnavailableError(), types.ErrorCodeSubscriptionUnavailable, "当前没有可用的订阅计划，请先开通或续费后再使用。", http.StatusForbidden},
		{"subscription expired", newSubscriptionExpiredError("专业版"), types.ErrorCodeSubscriptionExpired, "您的“专业版”已到期，请续费后继续使用。", http.StatusForbidden},
		{"subscription exhausted", newSubscriptionQuotaError(subscriptionQuotaErrorInput{Plan: "专业版", ResetAt: "2026-09-01 00:00"}), types.ErrorCodeSubscriptionPeriodExhausted, "您的“专业版”本周期额度已使用完毕，将于 2026-09-01 00:00 重置。", http.StatusForbidden},
		{"API token exhausted", newAPITokenQuotaError(true), types.ErrorCodeAPITokenQuotaExhausted, "当前 API 令牌额度已使用完毕，请更换令牌或联系管理员。", http.StatusForbidden},
		{"API token insufficient", newAPITokenQuotaError(false), types.ErrorCodeAPITokenQuotaInsufficient, "当前 API 令牌剩余额度不足以完成本次请求，请更换令牌或联系管理员。", http.StatusForbidden},
		{"usage check unavailable", newUsageLimitCheckUnavailableError(), types.ErrorCodeUsageLimitCheckUnavailable, "用量校验暂时不可用，请稍后重试。", http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.code, tt.err.GetErrorCode())
			require.Equal(t, tt.message, tt.err.Error())
			require.Equal(t, tt.status, tt.err.StatusCode)
		})
	}
}

func TestFormatTokenMillionsUsesExactIntegerArithmetic(t *testing.T) {
	require.Equal(t, "100", formatTokenMillions(100_000_000))
	require.Equal(t, "9007199254.740993", formatTokenMillions(9_007_199_254_740_993))
}
