package service

import (
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const modelQuotaUsageRecordedContextKey = "model_quota_usage_recorded"

func sumSettledTokens(input, output int) int64 {
	inputTokens, outputTokens := int64(input), int64(output)
	if inputTokens < 0 || outputTokens < 0 {
		return 0
	}
	if inputTokens > math.MaxInt64-outputTokens {
		return math.MaxInt64
	}
	return inputTokens + outputTokens
}

// recordModelQuotaFromContext reads the model quota usage IDs from gin context
// and synchronously records the actual settled amount and token consumption.
func recordModelQuotaFromContext(c *gin.Context, actualQuota int, actualTokens int64) error {
	if actualQuota < 0 || actualTokens < 0 {
		return fmt.Errorf("settled usage must be non-negative: quota=%d tokens=%d", actualQuota, actualTokens)
	}
	if actualQuota == 0 && actualTokens == 0 {
		return nil
	}
	if c.GetBool(modelQuotaUsageRecordedContextKey) {
		return nil
	}
	val, exists := c.Get("model_quota_usage_ids")
	if !exists {
		return nil
	}
	usageIds, ok := val.([]int)
	if !ok || len(usageIds) == 0 {
		return nil
	}
	if err := RecordModelQuotaUsage(usageIds, int64(actualQuota), actualTokens); err != nil {
		return err
	}
	c.Set(modelQuotaUsageRecordedContextKey, true)
	return nil
}

func TaskUsageContributionFromContext(c *gin.Context, actualQuota int) *model.TaskUsageContribution {
	if actualQuota < 0 {
		return nil
	}
	value, exists := c.Get(modelQuotaUsageContextKey)
	if !exists {
		return nil
	}
	usageIDs, ok := value.([]int)
	if !ok || len(usageIDs) == 0 {
		return nil
	}
	return &model.TaskUsageContribution{
		UsageIDs: append([]int(nil), usageIDs...), Quota: int64(actualQuota),
		UsageDate: time.Now().In(modelQuotaShanghaiLocation).Format("2006-01-02"),
	}
}
