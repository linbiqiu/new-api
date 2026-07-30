package codex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLAlphaSearch(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelBaseUrl: "https://chatgpt.com",
		},
		RelayMode: relayconstant.RelayModeAlphaSearch,
	}

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://chatgpt.com/backend-api/codex/alpha/search", url)
}

func TestSetupRequestHeaderPreservesCodexClientContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Originator", "Codex Desktop")
	c.Request.Header.Set("X-Codex-Beta-Features", "terminal_resize_reflow")
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"turn"}`)
	c.Request.Header.Set("X-Codex-Window-Id", "window-1")
	c.Request.Header.Set("X-Client-Request-Id", "request-1")
	c.Request.Header.Set("Session-Id", "session-1")
	c.Request.Header.Set("Thread-Id", "thread-1")

	info := &relaycommon.RelayInfo{
		IsStream:  true,
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: `{"access_token":"upstream-token","account_id":"account-1"}`,
		},
	}
	headers := make(http.Header)

	err := (&Adaptor{}).SetupRequestHeader(c, &headers, info)
	require.NoError(t, err)
	assert.Equal(t, "Bearer upstream-token", headers.Get("Authorization"))
	assert.Equal(t, "account-1", headers.Get("Chatgpt-Account-Id"))
	assert.Equal(t, "Codex Desktop", headers.Get("Originator"))
	assert.Equal(t, "terminal_resize_reflow", headers.Get("X-Codex-Beta-Features"))
	assert.JSONEq(t, `{"request_kind":"turn"}`, headers.Get("X-Codex-Turn-Metadata"))
	assert.Equal(t, "window-1", headers.Get("X-Codex-Window-Id"))
	assert.Equal(t, "request-1", headers.Get("X-Client-Request-Id"))
	assert.Equal(t, "session-1", headers.Get("Session-Id"))
	assert.Equal(t, "thread-1", headers.Get("Thread-Id"))
}
