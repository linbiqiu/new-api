package types

import (
	"errors"
	"net/http"
	"testing"
)

func TestQuotaErrorCodesToOpenAIError(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		want string
	}{
		{"wallet quota exhausted", ErrorCodeWalletQuotaExhausted, "wallet_quota_exhausted"},
		{"wallet quota insufficient", ErrorCodeWalletQuotaInsufficient, "wallet_quota_insufficient"},
		{"subscription unavailable", ErrorCodeSubscriptionUnavailable, "subscription_unavailable"},
		{"subscription expired", ErrorCodeSubscriptionExpired, "subscription_expired"},
		{"subscription period exhausted", ErrorCodeSubscriptionPeriodExhausted, "subscription_period_quota_exhausted"},
		{"subscription period insufficient", ErrorCodeSubscriptionPeriodInsufficient, "subscription_period_quota_insufficient"},
		{"API token quota exhausted", ErrorCodeAPITokenQuotaExhausted, "api_token_quota_exhausted"},
		{"API token quota insufficient", ErrorCodeAPITokenQuotaInsufficient, "api_token_quota_insufficient"},
		{"all models amount limit exhausted", ErrorCodeAllModelsAmountLimitExhausted, "all_models_amount_limit_exhausted"},
		{"all models amount limit insufficient", ErrorCodeAllModelsAmountLimitInsufficient, "all_models_amount_limit_insufficient"},
		{"all models token limit exhausted", ErrorCodeAllModelsTokenLimitExhausted, "all_models_token_limit_exhausted"},
		{"model amount limit exhausted", ErrorCodeModelAmountLimitExhausted, "model_amount_limit_exhausted"},
		{"model amount limit insufficient", ErrorCodeModelAmountLimitInsufficient, "model_amount_limit_insufficient"},
		{"usage limit check unavailable", ErrorCodeUsageLimitCheckUnavailable, "usage_limit_check_unavailable"},
	}

	const message = "当前账户余额已使用完毕，请充值后继续使用。"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.code) != tt.want {
				t.Fatalf("unexpected stable code: got %q, want %q", tt.code, tt.want)
			}

			err := NewErrorWithStatusCode(errors.New(message), tt.code, http.StatusForbidden)
			openAIError := err.ToOpenAIError()

			if openAIError.Message != message {
				t.Fatalf("unexpected message: %q", openAIError.Message)
			}
			if openAIError.Code != tt.code {
				t.Fatalf("unexpected code: %v", openAIError.Code)
			}
			if err.StatusCode != http.StatusForbidden {
				t.Fatalf("unexpected status code: %d", err.StatusCode)
			}
		})
	}
}
