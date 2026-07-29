package billingexpr_test

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComputeTieredQuota_ClampOnOverflow guards the billing-safety invariant
// that an oversized tiered settlement clamps to the int32 max instead of
// wrapping into a credit, and that the saturation event is surfaced on the
// result so callers can record it for admin auditing.
func TestComputeTieredQuota_ClampOnOverflow(t *testing.T) {
	// exprOutput = p * 1e9 = 1e18; quotaBeforeGroup = 1e18 / 1e6 * 5e5 = 5e17,
	// which far exceeds MaxInt32 and must saturate.
	exprStr := `tier("base", p * 1000000000)`
	snap := &billingexpr.BillingSnapshot{
		BillingMode:  "tiered_expr",
		ExprString:   exprStr,
		ExprHash:     billingexpr.ExprHashString(exprStr),
		GroupRatio:   1.0,
		QuotaPerUnit: 500_000,
	}

	result, err := billingexpr.ComputeTieredQuota(snap, billingexpr.TokenParams{P: 1_000_000_000})
	require.NoError(t, err)

	assert.Equal(t, math.MaxInt32, result.ActualQuotaAfterGroup, "oversized quota must clamp to int32 max, never wrap negative")
	require.NotNil(t, result.Clamp, "clamp event must be surfaced so it can be audited")
	assert.Equal(t, common.QuotaClampOverflow, result.Clamp.Kind)
	assert.Equal(t, math.MaxInt32, result.Clamp.Clamped)
}

// TestComputeTieredQuota_NoClampInRange confirms an in-range settlement leaves
// Clamp nil, so the audit path is a no-op in the common case.
func TestComputeTieredQuota_NoClampInRange(t *testing.T) {
	exprStr := `tier("base", p * 2 + c * 10)`
	snap := &billingexpr.BillingSnapshot{
		BillingMode:  "tiered_expr",
		ExprString:   exprStr,
		ExprHash:     billingexpr.ExprHashString(exprStr),
		GroupRatio:   1.0,
		QuotaPerUnit: 500_000,
	}

	result, err := billingexpr.ComputeTieredQuota(snap, billingexpr.TokenParams{P: 1000, C: 500})
	require.NoError(t, err)
	assert.Nil(t, result.Clamp, "in-range settlement must not report a clamp")
}

func TestComputeTieredQuotaRejectsNegativeCharge(t *testing.T) {
	exprStr := `c < 100 ? tier("invalid", -100) : tier("normal", c * 10)`
	snap := &billingexpr.BillingSnapshot{
		BillingMode:  "tiered_expr",
		ExprString:   exprStr,
		ExprHash:     billingexpr.ExprHashString(exprStr),
		GroupRatio:   1,
		QuotaPerUnit: 500_000,
	}

	_, err := billingexpr.ComputeTieredQuota(snap, billingexpr.TokenParams{C: 50})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be negative")
}

func TestRunExprWithRequestUsesFrozenEvaluationTime(t *testing.T) {
	evaluatedAt := time.Date(2026, time.July, 29, 15, 4, 0, 0, time.UTC)
	exprStr := `hour("UTC") == 15 && minute("UTC") == 4 && weekday("UTC") == 3 && month("UTC") == 7 && day("UTC") == 29 ? tier("frozen", p) : tier("other", 0)`

	cost, trace, err := billingexpr.RunExprWithRequest(
		exprStr,
		billingexpr.TokenParams{P: 123},
		billingexpr.RequestInput{EvaluatedAt: evaluatedAt},
	)
	require.NoError(t, err)
	assert.Equal(t, 123.0, cost)
	assert.Equal(t, "frozen", trace.MatchedTier)
}
