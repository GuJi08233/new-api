package gemini

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchTaskRedirectDoesNotForwardAPIKeyOrLogSignedLocation(t *testing.T) {
	setting := system_setting.GetFetchSetting()
	previous := *setting
	t.Cleanup(func() {
		*setting = previous
		service.ResetProxyClientCache()
	})
	for _, blocked := range []bool{false, true} {
		name := "anonymous redirect"
		if blocked {
			name = "blocked signed redirect"
		}
		t.Run(name, func(t *testing.T) {
			received := make(chan string, 1)
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- r.Header.Get("x-goog-api-key")
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(target.Close)
			sourceKeys := make(chan string, 1)
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceKeys <- r.Header.Get("x-goog-api-key")
				http.Redirect(w, r, target.URL+"?key=query-secret&signature=signature-secret", http.StatusFound)
			}))
			t.Cleanup(source.Close)
			setting.EnableSSRFProtection = blocked
			if blocked {
				sourceURL, err := url.Parse(source.URL)
				require.NoError(t, err)
				setting.AllowPrivateIp = true
				setting.DomainFilterMode, setting.IpFilterMode = false, false
				setting.DomainList, setting.IpList = nil, nil
				setting.AllowedPorts = []string{sourceURL.Port()}
			}
			adaptor := &TaskAdaptor{}
			response, err := adaptor.FetchTask(strings.Replace(source.URL, "127.0.0.1", "localhost", 1), "fictional-key", map[string]any{
				"task_id": taskcommon.EncodeLocalTaskID("operations/test"),
			}, "", true)
			require.Len(t, sourceKeys, 1)
			assert.Equal(t, "fictional-key", <-sourceKeys)
			if blocked {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "query-secret")
				assert.NotContains(t, err.Error(), "signature-secret")
				assert.Empty(t, received)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, response)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, http.StatusOK, response.StatusCode)
			assert.Empty(t, <-received)
		})
	}
}
