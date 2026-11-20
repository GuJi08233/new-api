package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPKCEChallengeS256(t *testing.T) {
	// Published RFC 7636 Appendix B test vector.
	assert.Equal(t, "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		pkceChallengeS256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
}

func TestGeneratePKCEPair(t *testing.T) {
	verifier, challenge, err := GeneratePKCEPair()
	require.NoError(t, err)
	assert.Regexp(t, `^[A-Za-z0-9._~-]{43,128}$`, verifier)
	assert.Equal(t, pkceChallengeS256(verifier), challenge)

	nextVerifier, _, err := GeneratePKCEPair()
	require.NoError(t, err)
	assert.NotEqual(t, verifier, nextVerifier)
}
