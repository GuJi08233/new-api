package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRiskRankingsRejectsInvalidMetric(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/risk/rankings?metric=unknown", nil)

	GetRiskRankings(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.JSONEq(t, `{"success":false,"message":"invalid risk metric"}`, recorder.Body.String())
}
