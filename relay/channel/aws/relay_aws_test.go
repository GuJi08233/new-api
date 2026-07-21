package aws

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	smithymiddleware "github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}

func TestAwsSDKOperationOptionsInjectsHeadersBeforeSigning(t *testing.T) {
	var received http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := bedrockruntime.New(bedrockruntime.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(server.URL),
		Credentials:  credentials.NewStaticCredentialsProvider("access-key", "secret-key", ""),
		HTTPClient:   server.Client(),
	})

	input := &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String("model-id"),
		Accept:      aws.String("application/json"),
		ContentType: aws.String("application/json"),
		Body:        []byte(`{}`),
	}
	requestHeaders := http.Header{
		"X-Trace-Id":    {"trace-123"},
		"Authorization": {"Bearer client-credential"},
		"Cookie":        {"session=must-not-forward"},
		"X-Amz-Date":    {"client-date"},
		"Content-Type":  {"text/plain"},
	}

	_, err := client.InvokeModel(context.Background(), input, awsSDKOperationOptions(requestHeaders)...)
	require.NoError(t, err)
	assert.Equal(t, "trace-123", received.Get("X-Trace-Id"))
	assert.Empty(t, received.Get("Cookie"))
	assert.NotEqual(t, "Bearer client-credential", received.Get("Authorization"))
	assert.Contains(t, received.Get("Authorization"), "SignedHeaders=")
	assert.Contains(t, received.Get("Authorization"), "x-trace-id")
	assert.NotEqual(t, "client-date", received.Get("X-Amz-Date"))
	assert.Equal(t, "application/json", received.Get("Content-Type"))
}

func TestCopyAwsResponseHeadersCopiesSDKStreamMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		IsStream: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
		},
	}

	rawResponse := &smithyhttp.Response{Response: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Upstream-Trace": {"trace-aws"},
			"Set-Cookie":       {"session=must-not-forward"},
		},
	}}
	addRawResponse := awsmiddleware.AddRawResponse{}
	_, metadata, err := addRawResponse.HandleDeserialize(
		context.Background(),
		smithymiddleware.DeserializeInput{},
		smithymiddleware.DeserializeHandlerFunc(func(context.Context, smithymiddleware.DeserializeInput) (smithymiddleware.DeserializeOutput, smithymiddleware.Metadata, error) {
			return smithymiddleware.DeserializeOutput{RawResponse: rawResponse}, smithymiddleware.Metadata{}, nil
		}),
	)
	require.NoError(t, err)

	copyAwsResponseHeaders(c, info, metadata)
	assert.Equal(t, "trace-aws", recorder.Header().Get("X-Upstream-Trace"))
	assert.Empty(t, recorder.Header().Get("Set-Cookie"))
}

func TestNewAwsInvokeContextForInheritsRequestCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(parent)

	invokeCtx, cancelInvoke := newAwsInvokeContextFor(c)
	defer cancelInvoke()
	require.ErrorIs(t, invokeCtx.Err(), context.Canceled)
}

func TestAwsStreamHandlerReturnsEventStreamReadErrorWithoutTerminalFrame(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.Header().Set("X-Amzn-Bedrock-Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("malformed-event-stream"))
	}))
	defer server.Close()

	client := bedrockruntime.New(bedrockruntime.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(server.URL),
		Credentials:  credentials.NewStaticCredentialsProvider("access-key", "secret-key", ""),
		HTTPClient:   server.Client(),
	})
	adaptor := &Adaptor{
		AwsClient: client,
		AwsReq: &bedrockruntime.InvokeModelWithResponseStreamInput{
			ModelId:     aws.String("model-id"),
			Accept:      aws.String("application/json"),
			ContentType: aws.String("application/json"),
			Body:        []byte(`{}`),
		},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		DisablePing: true,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}

	relayErr, usage := awsStreamHandler(c, info, adaptor)

	require.NotNil(t, relayErr)
	assert.Equal(t, types.ErrorCodeAwsInvokeError, relayErr.GetErrorCode())
	assert.Nil(t, usage)
	assert.False(t, strings.Contains(recorder.Body.String(), "[DONE]"))
}
