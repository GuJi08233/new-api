package helper

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type streamDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (r *streamDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.deadlines = append(r.deadlines, deadline)
	return nil
}

func TestStreamBudgetCarriesLargeEventAcrossWrites(t *testing.T) {
	now := time.Unix(1800000000, 0)
	var budget StreamWriteBudget
	assert.Equal(t, now.Add(31*time.Second), budget.Deadline(now, 8<<10))
	assert.Equal(t, now.Add(481*time.Second), budget.Deadline(now, 3600<<10))
	assert.Equal(t, now.Add(482*time.Second), budget.Deadline(now.Add(time.Minute), 8<<10))
	assert.Equal(t, now.Add(631*time.Second), budget.Deadline(now.Add(10*time.Minute), 8<<10))
}

func TestStreamFlushDoesNotShortenLargeEventWriteBudget(t *testing.T) {
	recorder := &streamDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	SetEventStreamHeaders(c)
	before := time.Now()
	require.NoError(t, StringData(c, strings.Repeat("x", 3600<<10)))
	ExtendWriteDeadline(c)
	require.NotEmpty(t, recorder.deadlines)
	assert.False(t, recorder.deadlines[len(recorder.deadlines)-1].Before(before.Add(480*time.Second)))
	assert.Contains(t, recorder.Body.String(), "data: xxx")
}
