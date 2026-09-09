package types

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetMessageReachesUpstreamErrorPayloads pins the contract the relay error
// path depends on: whatever SetMessage installs (request id suffix, model name
// masking) must be what the client actually receives. Upstream errors keep the
// provider's own RelayError struct, and To*Error returns it verbatim for the
// matching format, so overwriting only the wrapped error would leave the
// original upstream text on the wire.
func TestSetMessageReachesUpstreamErrorPayloads(t *testing.T) {
	const masked = "The model `gpt-4` does not exist (request id: abc)"

	t.Run("openai upstream error", func(t *testing.T) {
		apiErr := WithOpenAIError(OpenAIError{
			Message: "The model `gpt-4o-2024-08-06` does not exist",
			Type:    "invalid_request_error",
			Code:    "model_not_found",
		}, http.StatusNotFound)
		require.NotNil(t, apiErr)

		apiErr.SetMessage(masked)

		assert.Equal(t, masked, apiErr.ToOpenAIError().Message)
		assert.Equal(t, masked, apiErr.ToClaudeError().Message)
		// Non-message fields of the upstream payload stay intact.
		assert.Equal(t, "invalid_request_error", apiErr.ToOpenAIError().Type)
	})

	t.Run("claude upstream error", func(t *testing.T) {
		apiErr := WithClaudeError(ClaudeError{
			Message: "model: gpt-4o-2024-08-06 not found",
			Type:    "not_found_error",
		}, http.StatusNotFound)
		require.NotNil(t, apiErr)

		apiErr.SetMessage(masked)

		assert.Equal(t, masked, apiErr.ToClaudeError().Message)
		assert.Equal(t, masked, apiErr.ToOpenAIError().Message)
		assert.Equal(t, "not_found_error", apiErr.ToClaudeError().Type)
	})
}
