package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupModelQuotaControllerTest(t *testing.T) {
	t.Helper()
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Log{}, &model.ModelQuotaGroupRule{}, &model.ModelQuotaPlanRule{},
		&model.ModelQuotaUserRule{}, &model.UserModelQuotaUsage{},
	))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "admin", Status: common.UserStatusEnabled}).Error)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})
}

func modelQuotaHandlerContext(t *testing.T, method, path, body string, id int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 1)
	ctx.Set("username", "admin")
	ctx.Set("role", common.RoleRootUser)
	if id > 0 {
		ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(id)}}
	}
	return ctx, recorder
}

func requireAPIResponseSuccess(t *testing.T, recorder *httptest.ResponseRecorder, want bool) {
	t.Helper()
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, want, response.Success, recorder.Body.String())
}

func TestCreateAggregateRuleValidation(t *testing.T) {
	setupModelQuotaControllerTest(t)
	ctx, recorder := modelQuotaHandlerContext(t, http.MethodPost, "/api/model-quota/group-rules", `{
		"group_name":"default","scope":"all","period":"monthly","quota_limit":0,"token_limit":100000000
	}`, 0)
	CreateModelQuotaGroupRule(ctx)
	requireAPIResponseSuccess(t, recorder, true)
	var rule model.ModelQuotaGroupRule
	require.NoError(t, model.DB.First(&rule).Error)
	require.Equal(t, model.ModelQuotaScopeAll, rule.Scope)
	require.EqualValues(t, 100_000_000, rule.TokenLimit)

	ctx, recorder = modelQuotaHandlerContext(t, http.MethodPost, "/api/model-quota/group-rules", `{
		"group_name":"default","scope":"model","model_pattern":"gpt-5","quota_limit":0,"token_limit":100000000
	}`, 0)
	CreateModelQuotaGroupRule(ctx)
	requireAPIResponseSuccess(t, recorder, false)
}

func TestUpdateAggregateRulePreservesOrArchivesUsage(t *testing.T) {
	setupModelQuotaControllerTest(t)
	rule := model.ModelQuotaGroupRule{GroupName: "default", Scope: model.ModelQuotaScopeAll, ModelPattern: "*", MatchMode: model.ModelQuotaMatchModeExact, Period: model.ModelQuotaPeriodMonthly, QuotaLimit: 1000, TokenLimit: 1000, Enabled: true}
	require.NoError(t, model.DB.Create(&rule).Error)
	usage := model.UserModelQuotaUsage{
		UserId: 20, RuleId: rule.Id, RuleSource: model.ModelQuotaRuleSourceGroup,
		ModelPattern: "*", QuotaLimit: 1000, QuotaUsed: 400, TokenLimit: 1000, TokenUsed: 300,
		PeriodStart: 1, PeriodEnd: 4_102_444_800, Status: model.ModelQuotaUsageStatusActive,
	}
	require.NoError(t, model.DB.Create(&usage).Error)

	ctx, recorder := modelQuotaHandlerContext(t, http.MethodPut, "/api/model-quota/group-rules/1", `{
		"group_name":"default","scope":"all","period":"monthly","quota_limit":2000,"token_limit":3000,"enabled":true
	}`, rule.Id)
	UpdateModelQuotaGroupRule(ctx)
	requireAPIResponseSuccess(t, recorder, true)
	require.NoError(t, model.DB.First(&usage, usage.Id).Error)
	require.EqualValues(t, 400, usage.QuotaUsed)
	require.EqualValues(t, 300, usage.TokenUsed)
	require.EqualValues(t, 2000, usage.QuotaLimit)
	require.EqualValues(t, 3000, usage.TokenLimit)

	ctx, recorder = modelQuotaHandlerContext(t, http.MethodPut, "/api/model-quota/group-rules/1", `{
		"group_name":"default","scope":"all","period":"daily","quota_limit":2000,"token_limit":3000,"enabled":true
	}`, rule.Id)
	UpdateModelQuotaGroupRule(ctx)
	requireAPIResponseSuccess(t, recorder, true)
	require.NoError(t, model.DB.First(&usage, usage.Id).Error)
	require.Equal(t, model.ModelQuotaUsageStatusExpired, usage.Status)
}

func TestDeleteRulePreservesHistoryAndResetClearsBothMetrics(t *testing.T) {
	setupModelQuotaControllerTest(t)
	rule := model.ModelQuotaUserRule{UserId: 30, Scope: model.ModelQuotaScopeAll, Period: model.ModelQuotaPeriodTotal, QuotaLimit: 1000, TokenLimit: 1000, Enabled: true}
	require.NoError(t, model.DB.Create(&rule).Error)
	usage := model.UserModelQuotaUsage{
		UserId: 30, RuleId: rule.Id, RuleSource: model.ModelQuotaRuleSourceUser,
		ModelPattern: "*", QuotaLimit: 1000, QuotaUsed: 900, TokenLimit: 1000, TokenUsed: 800,
		PeriodStart: 1, PeriodEnd: 4_102_444_800, Status: model.ModelQuotaUsageStatusActive,
	}
	require.NoError(t, model.DB.Create(&usage).Error)

	ctx, recorder := modelQuotaHandlerContext(t, http.MethodDelete, "/api/model-quota/user-rules/1", "", rule.Id)
	DeleteModelQuotaUserRule(ctx)
	requireAPIResponseSuccess(t, recorder, true)
	require.NoError(t, model.DB.First(&usage, usage.Id).Error)

	ctx, recorder = modelQuotaHandlerContext(t, http.MethodPost, "/api/model-quota/user-usage/1/reset", "", usage.Id)
	ResetUserModelQuotaUsage(ctx)
	requireAPIResponseSuccess(t, recorder, true)
	require.NoError(t, model.DB.First(&usage, usage.Id).Error)
	require.Zero(t, usage.QuotaUsed)
	require.Zero(t, usage.TokenUsed)
}
