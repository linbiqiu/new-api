package middleware

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ModelQuotaLimitKey is the gin context key for model quota usage IDs
const ModelQuotaLimitKey = "model_quota_usage_ids"

// ModelQuotaLimit checks if the user has remaining model-specific quota
// before forwarding the request to upstream. It is an independent interceptor
// that does NOT participate in the billing pre-consume / settle / refund flow.
//
// After the request completes, the actual consumed quota is recorded via
// observation hooks in SettleBilling / ReturnPreConsumedQuota.
func ModelQuotaLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip if no model specified
		modelName := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
		if modelName == "" {
			c.Next()
			return
		}

		userId := c.GetInt("id")
		if userId == 0 {
			c.Next()
			return
		}

		userGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		if userGroup == "" {
			userGroup = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}

		// Estimate pre-consume quota (conservative: use model price if available)
		preQuota := estimateModelQuota(modelName)
		service.RecordUsageGovernanceCheck()

		result, err := service.CheckPreFundingModelQuota(userId, modelName, userGroup, preQuota)
		if err != nil {
			service.RecordUsageGovernanceUnavailable()
			common.SysError("model quota check error: " + err.Error())
			apiError := types.NewErrorWithStatusCode(
				errors.New("用量校验暂时不可用，请稍后重试。"),
				types.ErrorCodeUsageLimitCheckUnavailable,
				http.StatusServiceUnavailable,
			)
			c.JSON(apiError.StatusCode, gin.H{"error": apiError.ToOpenAIError()})
			c.Abort()
			return
		}

		if !result.Passed {
			service.RecordUsageGovernanceRejected()
			if result.APIError == nil {
				apiError := types.NewErrorWithStatusCode(
					errors.New("用量校验暂时不可用，请稍后重试。"),
					types.ErrorCodeUsageLimitCheckUnavailable,
					http.StatusServiceUnavailable,
				)
				c.JSON(apiError.StatusCode, gin.H{"error": apiError.ToOpenAIError()})
				c.Abort()
				return
			}
			c.JSON(result.APIError.StatusCode, gin.H{"error": result.APIError.ToOpenAIError()})
			c.Abort()
			return
		}

		// Store usage IDs for post-request recording
		if len(result.UsageIDs) > 0 {
			c.Set(ModelQuotaLimitKey, result.UsageIDs)
		}

		c.Next()
	}
}
