package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

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

type groupModelQuotaRuleInput struct {
	modelQuotaRuleInput
	GroupName string `json:"group_name"`
}

type planModelQuotaRuleInput struct {
	modelQuotaRuleInput
	PlanId int `json:"plan_id"`
}

type userModelQuotaRuleInput struct {
	modelQuotaRuleInput
	UserId   int    `json:"user_id"`
	Username string `json:"username"`
}

func normalizeModelQuotaRuleInput(input *modelQuotaRuleInput, hasPeriod bool) error {
	input.Scope = strings.TrimSpace(input.Scope)
	if input.Scope == "" {
		input.Scope = model.ModelQuotaScopeModel
	}
	if input.QuotaLimit < 0 || input.TokenLimit < 0 {
		return fmt.Errorf("金额和 Token 上限不能为负数")
	}
	switch input.Scope {
	case model.ModelQuotaScopeAll:
		if input.QuotaLimit == 0 && input.TokenLimit == 0 {
			return fmt.Errorf("全部模型规则至少需要设置金额或 Token 上限")
		}
		input.ModelPattern = "*"
		input.MatchMode = model.ModelQuotaMatchModeExact
	case model.ModelQuotaScopeModel:
		input.ModelPattern = strings.TrimSpace(input.ModelPattern)
		if input.ModelPattern == "" || input.QuotaLimit <= 0 {
			return fmt.Errorf("指定模型规则需要模型名称和正金额上限")
		}
		if input.MatchMode == "" {
			input.MatchMode = model.ModelQuotaMatchModeExact
		}
		if input.MatchMode != model.ModelQuotaMatchModeExact && input.MatchMode != model.ModelQuotaMatchModePrefix {
			return fmt.Errorf("不支持的模型匹配模式")
		}
		input.TokenLimit = 0
	default:
		return fmt.Errorf("不支持的限制范围")
	}
	if !hasPeriod {
		return nil
	}
	if input.Period == "" {
		input.Period = model.ModelQuotaPeriodTotal
	}
	switch input.Period {
	case model.ModelQuotaPeriodTotal, model.ModelQuotaPeriodDaily, model.ModelQuotaPeriodWeekly, model.ModelQuotaPeriodMonthly:
		return nil
	default:
		return fmt.Errorf("不支持的限制周期")
	}
}

func requestedEnabled(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func auditRuleMutation(c *gin.Context, action, source string, id int, target any, oldValue, newValue any) {
	recordManageAudit(c, action, map[string]interface{}{
		"rule_source": source, "rule_id": id, "target": target,
		"old": oldValue, "new": newValue,
	})
}

func GetModelQuotaGroupRules(c *gin.Context) {
	query := model.DB.Model(&model.ModelQuotaGroupRule{})
	if groupName := c.Query("group_name"); groupName != "" {
		query = query.Where("group_name = ?", groupName)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	var items []*model.ModelQuotaGroupRule
	if err := query.Order("sort_order ASC, id ASC").Offset(pageInfo.GetStartIdx()).Limit(pageInfo.GetPageSize()).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func CreateModelQuotaGroupRule(c *gin.Context) {
	var input groupModelQuotaRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	input.GroupName = strings.TrimSpace(input.GroupName)
	if input.GroupName == "" {
		common.ApiErrorMsg(c, "用户分组不能为空")
		return
	}
	if err := normalizeModelQuotaRuleInput(&input.modelQuotaRuleInput, true); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	rule := model.ModelQuotaGroupRule{
		GroupName: input.GroupName, Scope: input.Scope, ModelPattern: input.ModelPattern,
		MatchMode: input.MatchMode, Period: input.Period, QuotaLimit: input.QuotaLimit,
		TokenLimit: input.TokenLimit, Enabled: requestedEnabled(input.Enabled, true), SortOrder: input.SortOrder,
	}
	if err := model.DB.Create(&rule).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	auditRuleMutation(c, "usage_limit.rule.create", model.ModelQuotaRuleSourceGroup, rule.Id, rule.GroupName, nil, rule)
	common.ApiSuccess(c, rule)
}

func UpdateModelQuotaGroupRule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var old model.ModelQuotaGroupRule
	if id <= 0 || model.DB.First(&old, id).Error != nil {
		common.ApiErrorMsg(c, "规则不存在")
		return
	}
	var input groupModelQuotaRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	input.GroupName = strings.TrimSpace(input.GroupName)
	if input.GroupName == "" {
		common.ApiErrorMsg(c, "用户分组不能为空")
		return
	}
	if err := normalizeModelQuotaRuleInput(&input.modelQuotaRuleInput, true); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	updated := old
	updated.GroupName, updated.Scope, updated.ModelPattern = input.GroupName, input.Scope, input.ModelPattern
	updated.MatchMode, updated.Period = input.MatchMode, input.Period
	updated.QuotaLimit, updated.TokenLimit = input.QuotaLimit, input.TokenLimit
	updated.Enabled, updated.SortOrder = requestedEnabled(input.Enabled, old.Enabled), input.SortOrder
	identityChanged := old.GroupName != updated.GroupName || old.Scope != updated.Scope || old.ModelPattern != updated.ModelPattern || old.MatchMode != updated.MatchMode || old.Period != updated.Period
	if err := model.DB.Model(&old).Select("group_name", "scope", "model_pattern", "match_mode", "period", "quota_limit", "token_limit", "enabled", "sort_order").Updates(&updated).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var usageErr error
	if identityChanged {
		usageErr = model.ArchiveUserModelQuotaUsageByRule(id, model.ModelQuotaRuleSourceGroup)
	} else {
		usageErr = model.SyncUserModelQuotaLimitsByRule(id, model.ModelQuotaRuleSourceGroup, updated.QuotaLimit, updated.TokenLimit)
	}
	if usageErr != nil {
		common.ApiError(c, usageErr)
		return
	}
	action := "usage_limit.rule.update"
	if old.Enabled != updated.Enabled && !identityChanged && old.QuotaLimit == updated.QuotaLimit && old.TokenLimit == updated.TokenLimit {
		action = "usage_limit.rule.enable"
	}
	auditRuleMutation(c, action, model.ModelQuotaRuleSourceGroup, id, updated.GroupName, old, updated)
	common.ApiSuccess(c, gin.H{"id": id})
}

func DeleteModelQuotaGroupRule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var old model.ModelQuotaGroupRule
	if id <= 0 || model.DB.First(&old, id).Error != nil {
		common.ApiErrorMsg(c, "规则不存在")
		return
	}
	if err := model.DB.Delete(&old).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	auditRuleMutation(c, "usage_limit.rule.delete", model.ModelQuotaRuleSourceGroup, id, old.GroupName, old, nil)
	common.ApiSuccess(c, gin.H{"id": id})
}

func GetModelQuotaPlanRules(c *gin.Context) {
	query := model.DB.Model(&model.ModelQuotaPlanRule{})
	if planId, _ := strconv.Atoi(c.Query("plan_id")); planId > 0 {
		query = query.Where("plan_id = ?", planId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	var items []*model.ModelQuotaPlanRule
	if err := query.Order("sort_order ASC, id ASC").Offset(pageInfo.GetStartIdx()).Limit(pageInfo.GetPageSize()).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func CreateModelQuotaPlanRule(c *gin.Context) {
	var input planModelQuotaRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	if input.PlanId <= 0 {
		common.ApiErrorMsg(c, "订阅计划不能为空")
		return
	}
	if err := normalizeModelQuotaRuleInput(&input.modelQuotaRuleInput, false); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	rule := model.ModelQuotaPlanRule{
		PlanId: input.PlanId, Scope: input.Scope, ModelPattern: input.ModelPattern,
		MatchMode: input.MatchMode, QuotaLimit: input.QuotaLimit, TokenLimit: input.TokenLimit,
		Enabled: requestedEnabled(input.Enabled, true), SortOrder: input.SortOrder,
	}
	if err := model.DB.Create(&rule).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	auditRuleMutation(c, "usage_limit.rule.create", model.ModelQuotaRuleSourcePlan, rule.Id, rule.PlanId, nil, rule)
	common.ApiSuccess(c, rule)
}

func UpdateModelQuotaPlanRule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var old model.ModelQuotaPlanRule
	if id <= 0 || model.DB.First(&old, id).Error != nil {
		common.ApiErrorMsg(c, "规则不存在")
		return
	}
	var input planModelQuotaRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	if input.PlanId <= 0 {
		common.ApiErrorMsg(c, "订阅计划不能为空")
		return
	}
	if err := normalizeModelQuotaRuleInput(&input.modelQuotaRuleInput, false); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	updated := old
	updated.PlanId, updated.Scope, updated.ModelPattern = input.PlanId, input.Scope, input.ModelPattern
	updated.MatchMode, updated.QuotaLimit, updated.TokenLimit = input.MatchMode, input.QuotaLimit, input.TokenLimit
	updated.Enabled, updated.SortOrder = requestedEnabled(input.Enabled, old.Enabled), input.SortOrder
	identityChanged := old.PlanId != updated.PlanId || old.Scope != updated.Scope || old.ModelPattern != updated.ModelPattern || old.MatchMode != updated.MatchMode
	if err := model.DB.Model(&old).Select("plan_id", "scope", "model_pattern", "match_mode", "quota_limit", "token_limit", "enabled", "sort_order").Updates(&updated).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var usageErr error
	if identityChanged {
		usageErr = model.ArchiveUserModelQuotaUsageByRule(id, model.ModelQuotaRuleSourcePlan)
	} else {
		usageErr = model.SyncUserModelQuotaLimitsByRule(id, model.ModelQuotaRuleSourcePlan, updated.QuotaLimit, updated.TokenLimit)
	}
	if usageErr != nil {
		common.ApiError(c, usageErr)
		return
	}
	action := "usage_limit.rule.update"
	if old.Enabled != updated.Enabled && !identityChanged && old.QuotaLimit == updated.QuotaLimit && old.TokenLimit == updated.TokenLimit {
		action = "usage_limit.rule.enable"
	}
	auditRuleMutation(c, action, model.ModelQuotaRuleSourcePlan, id, updated.PlanId, old, updated)
	common.ApiSuccess(c, gin.H{"id": id})
}

func DeleteModelQuotaPlanRule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var old model.ModelQuotaPlanRule
	if id <= 0 || model.DB.First(&old, id).Error != nil {
		common.ApiErrorMsg(c, "规则不存在")
		return
	}
	if err := model.DB.Delete(&old).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	auditRuleMutation(c, "usage_limit.rule.delete", model.ModelQuotaRuleSourcePlan, id, old.PlanId, old, nil)
	common.ApiSuccess(c, gin.H{"id": id})
}

func GetModelQuotaUserRules(c *gin.Context) {
	query := model.DB.Model(&model.ModelQuotaUserRule{})
	if userId, _ := strconv.Atoi(c.Query("user_id")); userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if username := c.Query("username"); username != "" {
		query = query.Where("username LIKE ?", "%"+username+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	var items []*model.ModelQuotaUserRule
	if err := query.Order("sort_order ASC, id ASC").Offset(pageInfo.GetStartIdx()).Limit(pageInfo.GetPageSize()).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func resolveRuleUsername(userId int, username string) string {
	if strings.TrimSpace(username) != "" {
		return strings.TrimSpace(username)
	}
	var user model.User
	if err := model.DB.Select("username").First(&user, userId).Error; err == nil {
		return user.Username
	}
	return ""
}

func CreateModelQuotaUserRule(c *gin.Context) {
	var input userModelQuotaRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	if input.UserId <= 0 {
		common.ApiErrorMsg(c, "用户不能为空")
		return
	}
	if err := normalizeModelQuotaRuleInput(&input.modelQuotaRuleInput, true); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	rule := model.ModelQuotaUserRule{
		UserId: input.UserId, Username: resolveRuleUsername(input.UserId, input.Username),
		Scope: input.Scope, ModelPattern: input.ModelPattern, MatchMode: input.MatchMode,
		Period: input.Period, QuotaLimit: input.QuotaLimit, TokenLimit: input.TokenLimit,
		Enabled: requestedEnabled(input.Enabled, true), SortOrder: input.SortOrder,
	}
	if err := model.DB.Create(&rule).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	auditRuleMutation(c, "usage_limit.rule.create", model.ModelQuotaRuleSourceUser, rule.Id, rule.UserId, nil, rule)
	common.ApiSuccess(c, rule)
}

func UpdateModelQuotaUserRule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var old model.ModelQuotaUserRule
	if id <= 0 || model.DB.First(&old, id).Error != nil {
		common.ApiErrorMsg(c, "规则不存在")
		return
	}
	var input userModelQuotaRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	if input.UserId <= 0 {
		common.ApiErrorMsg(c, "用户不能为空")
		return
	}
	if err := normalizeModelQuotaRuleInput(&input.modelQuotaRuleInput, true); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	updated := old
	updated.UserId, updated.Username = input.UserId, resolveRuleUsername(input.UserId, input.Username)
	updated.Scope, updated.ModelPattern, updated.MatchMode = input.Scope, input.ModelPattern, input.MatchMode
	updated.Period, updated.QuotaLimit, updated.TokenLimit = input.Period, input.QuotaLimit, input.TokenLimit
	updated.Enabled, updated.SortOrder = requestedEnabled(input.Enabled, old.Enabled), input.SortOrder
	identityChanged := old.UserId != updated.UserId || old.Scope != updated.Scope || old.ModelPattern != updated.ModelPattern || old.MatchMode != updated.MatchMode || old.Period != updated.Period
	if err := model.DB.Model(&old).Select("user_id", "username", "scope", "model_pattern", "match_mode", "period", "quota_limit", "token_limit", "enabled", "sort_order").Updates(&updated).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var usageErr error
	if identityChanged {
		usageErr = model.ArchiveUserModelQuotaUsageByRule(id, model.ModelQuotaRuleSourceUser)
	} else {
		usageErr = model.SyncUserModelQuotaLimitsByRule(id, model.ModelQuotaRuleSourceUser, updated.QuotaLimit, updated.TokenLimit)
	}
	if usageErr != nil {
		common.ApiError(c, usageErr)
		return
	}
	action := "usage_limit.rule.update"
	if old.Enabled != updated.Enabled && !identityChanged && old.QuotaLimit == updated.QuotaLimit && old.TokenLimit == updated.TokenLimit {
		action = "usage_limit.rule.enable"
	}
	auditRuleMutation(c, action, model.ModelQuotaRuleSourceUser, id, updated.UserId, old, updated)
	common.ApiSuccess(c, gin.H{"id": id})
}

func DeleteModelQuotaUserRule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var old model.ModelQuotaUserRule
	if id <= 0 || model.DB.First(&old, id).Error != nil {
		common.ApiErrorMsg(c, "规则不存在")
		return
	}
	if err := model.DB.Delete(&old).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	auditRuleMutation(c, "usage_limit.rule.delete", model.ModelQuotaRuleSourceUser, id, old.UserId, old, nil)
	common.ApiSuccess(c, gin.H{"id": id})
}

func GetUserModelQuotaUsage(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Query("user_id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "user_id is required")
		return
	}
	usages, err := model.GetUserModelQuotaUsageByUserId(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	type usageWithExtra struct {
		*model.UserModelQuotaUsage
		QuotaRemain       int64   `json:"quota_remain"`
		QuotaUsagePercent float64 `json:"quota_usage_percent"`
		TokenRemain       int64   `json:"token_remain"`
		TokenUsagePercent float64 `json:"token_usage_percent"`
	}
	items := make([]*usageWithExtra, 0, len(usages))
	for _, usage := range usages {
		quotaRemain := usage.QuotaLimit - usage.QuotaUsed
		if quotaRemain < 0 {
			quotaRemain = 0
		}
		tokenRemain := usage.TokenLimit - usage.TokenUsed
		if tokenRemain < 0 {
			tokenRemain = 0
		}
		item := &usageWithExtra{UserModelQuotaUsage: usage, QuotaRemain: quotaRemain, TokenRemain: tokenRemain}
		if usage.QuotaLimit > 0 {
			item.QuotaUsagePercent = float64(usage.QuotaUsed) / float64(usage.QuotaLimit) * 100
		}
		if usage.TokenLimit > 0 {
			item.TokenUsagePercent = float64(usage.TokenUsed) / float64(usage.TokenLimit) * 100
		}
		items = append(items, item)
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

func ResetUserModelQuotaUsage(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var usage model.UserModelQuotaUsage
	if id <= 0 || model.DB.First(&usage, id).Error != nil {
		common.ApiErrorMsg(c, "用量记录不存在")
		return
	}
	old := usage
	if err := model.ResetUserModelQuotaUsage(id); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, usage.UserId, "usage_limit.usage.reset", map[string]interface{}{
		"usage_id": id, "rule_source": usage.RuleSource, "rule_id": usage.RuleId,
		"old": old, "new": map[string]int64{"quota_used": 0, "token_used": 0},
	})
	common.ApiSuccess(c, gin.H{"id": id})
}

func GetUsageGovernanceMetrics(c *gin.Context) {
	common.ApiSuccess(c, service.SnapshotUsageGovernanceMetrics())
}
