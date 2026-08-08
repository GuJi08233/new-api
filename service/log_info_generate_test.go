package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoRecordsKnownDocumentCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		known      bool
		count      int
		wantLogged bool
	}{
		{name: "embedding batch", known: true, count: 3, wantLogged: true},
		{name: "known empty batch", known: true, count: 0, wantLogged: true},
		{name: "non document request", known: false, wantLogged: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.known {
				common.SetContextKey(ctx, constant.ContextKeyEstimatedDocs, tt.count)
			}
			startTime := time.Unix(100, 0)
			relayInfo := &relaycommon.RelayInfo{
				StartTime:         startTime,
				FirstResponseTime: startTime.Add(time.Millisecond),
				ChannelMeta:       &relaycommon.ChannelMeta{},
			}

			other := GenerateTextOtherInfo(ctx, relayInfo, 0, 1, 0, 0, 0, 0, 1)
			require.NotNil(t, other)
			count, found := other["document_count"]

			assert.Equal(t, tt.wantLogged, found)
			if tt.wantLogged {
				assert.Equal(t, tt.count, count)
			}
		})
	}
}
