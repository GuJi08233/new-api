package middleware

import (
	"net/http/httptest"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
)

func TestRouteWillEstimateTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		path      string
		relayMode int
		want      bool
	}{
		{name: "chat relay", path: "/v1/chat/completions", want: true},
		{name: "claude messages relay", path: "/v1/messages", want: true},
		{name: "gemini relay", path: "/v1beta/models/gemini-2.5-pro:generateContent", want: true},
		{name: "midjourney task", path: "/mj/submit/imagine", want: false},
		{name: "suno task", path: "/suno/submit/music", relayMode: relayconstant.RelayModeSunoSubmit, want: false},
		{name: "video task", path: "/v1/videos", relayMode: relayconstant.RelayModeVideoSubmit, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", tt.path, nil)
			if tt.relayMode != relayconstant.RelayModeUnknown {
				ctx.Set("relay_mode", tt.relayMode)
			}
			if got := routeWillEstimateTokens(ctx); got != tt.want {
				t.Fatalf("routeWillEstimateTokens(%q) = %t, want %t", tt.path, got, tt.want)
			}
		})
	}
}
