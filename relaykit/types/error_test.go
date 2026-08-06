package types

import (
	"errors"
	"net/http"
	"testing"
)

func TestWalletQuotaExhaustedToOpenAIError(t *testing.T) {
	err := NewErrorWithStatusCode(
		errors.New("当前账户余额已使用完毕，请充值后继续使用。"),
		ErrorCodeWalletQuotaExhausted,
		http.StatusForbidden,
	)
	openAIError := err.ToOpenAIError()

	if openAIError.Message != "当前账户余额已使用完毕，请充值后继续使用。" {
		t.Fatalf("unexpected message: %q", openAIError.Message)
	}
	if openAIError.Code != ErrorCodeWalletQuotaExhausted {
		t.Fatalf("unexpected code: %v", openAIError.Code)
	}
	if err.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected status code: %d", err.StatusCode)
	}
}
