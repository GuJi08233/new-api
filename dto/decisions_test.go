package dto

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecisionsNativeRequestContracts(t *testing.T) {
	tests := []struct {
		name   string
		format types.RelayFormat
		body   string
		err    string
	}{
		{"gemini noul", types.RelayFormatGeminiDecisions, `{"model":"gemini-decision-1","input":"damaged package","questions":{"damaged":{"type":"noul","instructions":"Is it damaged?"}}}`, ""},
		{"gemini choice and score", types.RelayFormatGeminiDecisions, `{"model":"gemini-decision-1","input":{"type":"text","text":"damaged package"},"questions":{"queue":{"type":"choice","instructions":"Which queue?","criteria":{"repair":"","other":"Other requests"}},"severity":{"type":"score","instructions":"How severe?","criteria":["minor","major"]}}}`, ""},
		{"gemini requires instructions", types.RelayFormatGeminiDecisions, `{"model":"gemini-decision-1","input":"hello","questions":{"q":{"type":"noul"}}}`, "instructions"},
		{"gemini rejects questions array", types.RelayFormatGeminiDecisions, `{"model":"gemini-decision-1","input":"hello","questions":[{"type":"predicate","instructions":"Greeting?"}]}`, "cannot unmarshal"},
		{"gemini rejects predicate", types.RelayFormatGeminiDecisions, `{"model":"gemini-decision-1","input":"hello","questions":{"q":{"type":"predicate","instructions":"Greeting?"}}}`, "noul, choice, or score"},
		{"gemini rejects empty input", types.RelayFormatGeminiDecisions, `{"model":"gemini-decision-1","input":"","questions":{"q":{"type":"noul","instructions":"Greeting?"}}}`, "empty"},
		{"gemini validates question key", types.RelayFormatGeminiDecisions, `{"model":"gemini-decision-1","input":"hello","questions":{"invalid key":{"type":"noul","instructions":"Greeting?"}}}`, "question keys"},
		{"openai unnamed predicate", types.RelayFormatOpenAIDecisions, `{"model":"gpt-6-luna","input":"hello","questions":[{"type":"predicate","instructions":"Greeting?"}]}`, ""},
		{"openai permits empty evidence", types.RelayFormatOpenAIDecisions, `{"model":"gpt-6-luna","input":"","questions":[{"type":"predicate","instructions":"Is 1 less than 2?"}]}`, ""},
		{"openai rejects missing input", types.RelayFormatOpenAIDecisions, `{"model":"gpt-6-luna","questions":[{"type":"predicate","instructions":"Greeting?"}]}`, "input"},
		{"openai rejects questions object", types.RelayFormatOpenAIDecisions, `{"model":"gpt-6-luna","input":"hello","questions":{"q":{"type":"noul","instructions":"Greeting?"}}}`, "cannot unmarshal"},
		{"openai rejects noul", types.RelayFormatOpenAIDecisions, `{"model":"gpt-6-luna","input":"hello","questions":[{"type":"noul","instructions":"Greeting?"}]}`, "predicate, choice, or score"},
		{"openai rejects file content", types.RelayFormatOpenAIDecisions, `{"model":"gpt-6-luna","input":[{"role":"user","content":[{"type":"input_file","file_id":"file-test"}]}],"questions":[{"type":"predicate","instructions":"Relevant?"}]}`, "unsupported content type"},
		{"openai rejects duplicate choices", types.RelayFormatOpenAIDecisions, `{"model":"gpt-6-luna","input":"hello","questions":[{"type":"choice","instructions":"Select","choices":[{"value":"a"},{"value":"a"}]}]}`, "unique"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := ParseDecisionsRequest([]byte(tt.body), tt.format)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.False(t, request.IsStream(nil))
			body, err := common.Marshal(request)
			require.NoError(t, err)
			assert.JSONEq(t, tt.body, string(body))
		})
	}
}

func TestDecisionsRequestQuestionLimits(t *testing.T) {
	for _, tt := range []struct {
		format types.RelayFormat
		limit  int
	}{{types.RelayFormatGeminiDecisions, 1000}, {types.RelayFormatOpenAIDecisions, 400}} {
		for _, count := range []int{tt.limit, tt.limit + 1} {
			t.Run(fmt.Sprintf("%s/%d", tt.format, count), func(t *testing.T) {
				var request DecisionsRequest
				if tt.format == types.RelayFormatGeminiDecisions {
					questions := make(map[string]GeminiDecisionQuestion, count)
					for i := 0; i < count; i++ {
						questions[fmt.Sprintf("q%d", i)] = GeminiDecisionQuestion{Type: "noul", Instructions: json.RawMessage(`"Is this a greeting?"`)}
					}
					request = &GeminiDecisionsRequest{Model: "gemini-decision-1", Input: json.RawMessage(`"hello"`), Questions: questions}
				} else {
					questions := make([]OpenAIDecisionQuestion, count)
					for i := range questions {
						questions[i] = OpenAIDecisionQuestion{Type: "predicate", Instructions: json.RawMessage(`"Is this a greeting?"`)}
					}
					request = &OpenAIDecisionsRequest{Model: "gpt-6-luna", Input: json.RawMessage(`"hello"`), Questions: questions}
				}
				if count > tt.limit {
					require.ErrorContains(t, request.Validate(), "questions must contain")
				} else {
					require.NoError(t, request.Validate())
				}
			})
		}
	}
}

func TestDecisionsScoreLevelLimits(t *testing.T) {
	for _, tt := range []struct {
		format types.RelayFormat
		limit  int
	}{{types.RelayFormatGeminiDecisions, 255}, {types.RelayFormatOpenAIDecisions, 10}} {
		for _, count := range []int{1, 2, tt.limit, tt.limit + 1} {
			t.Run(fmt.Sprintf("%s/%d", tt.format, count), func(t *testing.T) {
				var request DecisionsRequest
				if tt.format == types.RelayFormatGeminiDecisions {
					levels := make([]string, count)
					for i := range levels {
						levels[i] = fmt.Sprintf("Level %d", i)
					}
					criteria, err := common.Marshal(levels)
					require.NoError(t, err)
					request = &GeminiDecisionsRequest{Model: "gemini-decision-1", Input: json.RawMessage(`"hello"`), Questions: map[string]GeminiDecisionQuestion{"q": {Type: "score", Instructions: json.RawMessage(`"Quality?"`), Criteria: criteria}}}
				} else {
					levels := make([]OpenAIDecisionLevel, count)
					for i := range levels {
						label := fmt.Sprintf("Level %d", i)
						levels[i].Label = &label
					}
					request = &OpenAIDecisionsRequest{Model: "gpt-6-luna", Input: json.RawMessage(`"hello"`), Questions: []OpenAIDecisionQuestion{{Type: "score", Instructions: json.RawMessage(`"Quality?"`), Levels: levels}}}
				}
				if count < 2 || count > tt.limit {
					require.Error(t, request.Validate())
				} else {
					require.NoError(t, request.Validate())
				}
			})
		}
	}
}

func TestDecisionsChoiceOptionLimits(t *testing.T) {
	for _, count := range []int{1, 2, 255, 256} {
		criteria := make(map[string]string, count)
		choices := make([]OpenAIDecisionChoice, count)
		for i := range choices {
			option := fmt.Sprintf("option_%d", i)
			criteria[option] = ""
			value, err := common.Marshal(option)
			require.NoError(t, err)
			choices[i].Value = value
		}
		criteriaJSON, err := common.Marshal(criteria)
		require.NoError(t, err)
		requests := []DecisionsRequest{
			&GeminiDecisionsRequest{Model: "gemini-decision-1", Input: json.RawMessage(`"hello"`), Questions: map[string]GeminiDecisionQuestion{"q": {Type: "choice", Instructions: json.RawMessage(`"Choose"`), Criteria: criteriaJSON}}},
			&OpenAIDecisionsRequest{Model: "gpt-6-luna", Input: json.RawMessage(`"hello"`), Questions: []OpenAIDecisionQuestion{{Type: "choice", Instructions: json.RawMessage(`"Choose"`), Choices: choices}}},
		}
		for _, request := range requests {
			if count < 2 || count > 255 {
				require.Error(t, request.Validate())
			} else {
				require.NoError(t, request.Validate())
			}
		}
	}
}

func TestOpenAIDecisionsPreservesBooleanChoicesAndCacheControls(t *testing.T) {
	body := `{"model":"gpt-6-luna","input":[{"role":"user","content":[{"type":"input_json","data":{"count":0,"enabled":false}},{"type":"input_image","image_url":"https://example.com/package.png","detail":"low"}]}],"questions":[{"type":"choice","name":"","instructions":[{"type":"input_text","text":"Which value applies?"},{"type":"input_image","image_url":"https://example.com/reference.png"}],"choices":[{"value":false,"description":""},{"value":"false"},{"value":true}]}],"prompt_cache_key":"cache-test","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"safety_identifier":"test-user"}`
	request, err := ParseDecisionsRequest([]byte(body), types.RelayFormatOpenAIDecisions)
	require.NoError(t, err)
	forwarded, err := common.Marshal(request)
	require.NoError(t, err)
	assert.JSONEq(t, body, string(forwarded))
	meta := request.GetTokenCountMeta()
	assert.Len(t, meta.Files, 2)
	assert.Contains(t, meta.CombineText, `"count":0`)
	assert.Contains(t, meta.CombineText, `"enabled":false`)
	assert.Contains(t, meta.CombineText, "Which value applies?")
}

func TestOpenAIDecisionsCountsToolHistory(t *testing.T) {
	body := `{"model":"gpt-6-luna","input":[{"type":"function_call","call_id":"c1","name":"lookup_order","arguments":"{\"order\":7}"},{"type":"function_call_output","call_id":"c1","output":"package damaged"},{"type":"custom_tool_call","call_id":"c2","name":"search","input":"replacement policy"},{"type":"custom_tool_call_output","call_id":"c2","output":"replacement allowed"}],"questions":[{"type":"predicate","name":"hidden_question_id","instructions":"Can we replace it?"}]}`
	request, err := ParseDecisionsRequest([]byte(body), types.RelayFormatOpenAIDecisions)
	require.NoError(t, err)
	meta := request.GetTokenCountMeta()
	for _, text := range []string{"lookup_order", `"order":7`, "package damaged", "search", "replacement policy", "replacement allowed", "Can we replace it?"} {
		assert.Contains(t, meta.CombineText, text)
	}
	assert.NotContains(t, meta.CombineText, "hidden_question_id")
}

func TestGeminiDecisionsCountsMultimodalInputAndExcludesQuestionKeys(t *testing.T) {
	body := `{"model":"gemini-decision-1","input":[{"type":"text","text":"customer "},{"type":"text","text":"evidence"},{"type":"image","data":"AQ==","mime_type":"image/png","resolution":"low"},{"type":"audio","uri":"https://example.com/call.wav","mime_type":"audio/wav"},{"type":"video","uri":"https://example.com/clip.mp4","mime_type":"video/mp4"},{"type":"document","data":"AQ==","mime_type":"application/pdf"}],"questions":{"hidden_question_id":{"type":"noul","instructions":[{"type":"text","text":"Was it "},{"type":"text","text":"damaged?"}]}}}`
	request, err := ParseDecisionsRequest([]byte(body), types.RelayFormatGeminiDecisions)
	require.NoError(t, err)
	meta := request.GetTokenCountMeta()
	assert.Equal(t, "customer evidence\nWas it damaged?", meta.CombineText)
	require.Len(t, meta.Files, 4)
	assert.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	assert.Equal(t, types.FileTypeAudio, meta.Files[1].FileType)
	assert.Equal(t, types.FileTypeVideo, meta.Files[2].FileType)
	assert.Equal(t, types.FileTypeFile, meta.Files[3].FileType)
	assert.NotContains(t, meta.CombineText, "hidden_question_id")
}
