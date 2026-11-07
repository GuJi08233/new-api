package typesafe

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Providers implementing the System One API document their base URL either as
// https://host or https://host/v1, so both spellings must reach the same path
// instead of producing /v1/v1/systemone.
func TestGetRequestURLAcceptsBothBaseURLSpellings(t *testing.T) {
	testCases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "host only", baseURL: "https://api.subq.ai", want: "https://api.subq.ai/v1/systemone"},
		{name: "trailing slash", baseURL: "https://api.subq.ai/", want: "https://api.subq.ai/v1/systemone"},
		{name: "versioned base url", baseURL: "https://api.subq.ai/v1", want: "https://api.subq.ai/v1/systemone"},
		{name: "versioned base url with slash", baseURL: "https://api.subq.ai/v1/", want: "https://api.subq.ai/v1/systemone"},
		{name: "sub path", baseURL: "https://gateway.example.com/typesafe", want: "https://gateway.example.com/typesafe/v1/systemone"},
		{name: "full endpoint", baseURL: "https://api.subq.ai/v1/systemone", want: "https://api.subq.ai/v1/systemone"},
		{name: "full endpoint with slash", baseURL: "https://api.subq.ai/v1/systemone/", want: "https://api.subq.ai/v1/systemone"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatTypeSafe,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: testCase.baseURL},
			}

			got, err := (&Adaptor{}).GetRequestURL(info)

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}
