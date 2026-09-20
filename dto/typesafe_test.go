package dto

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypeSafeRequestValidate(t *testing.T) {
	noul := TypeSafeQuestion{Type: TypeSafeQuestionNoul, Instructions: json.RawMessage(`"Does this convey urgency?"`)}

	testCases := []struct {
		name    string
		request TypeSafeRequest
		wantErr string
	}{
		{
			name: "minimal noul question",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{"is_urgent": noul},
			},
		},
		{
			name: "structured state is accepted",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`[{"role":"user","text":"hi"}]`),
				Questions: map[string]TypeSafeQuestion{"is_urgent": noul},
			},
		},
		{
			name: "missing model",
			request: TypeSafeRequest{
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{"is_urgent": noul},
			},
			wantErr: "model is required",
		},
		{
			name: "missing state",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				Questions: map[string]TypeSafeQuestion{"is_urgent": noul},
			},
			wantErr: "state is required",
		},
		{
			name: "null state",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`null`),
				Questions: map[string]TypeSafeQuestion{"is_urgent": noul},
			},
			wantErr: "state is required",
		},
		{
			name: "empty questions",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{},
			},
			wantErr: "questions is required",
		},
		{
			name: "blank question id",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{"  ": noul},
			},
			wantErr: "question id must not be empty",
		},
		{
			name: "unknown question type",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"mystery": {Type: "ranking", Instructions: json.RawMessage(`"rank it"`)},
				},
			},
			wantErr: "type must be one of noul, choice, score",
		},
		{
			name: "noul with neither criteria nor instructions",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"is_urgent": {Type: TypeSafeQuestionNoul},
				},
			},
			wantErr: "must have criteria or instructions",
		},
		{
			name: "noul with blank instructions and no criteria",
			request: TypeSafeRequest{
				Model: "jev-latest",
				State: json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"is_urgent": {Type: TypeSafeQuestionNoul, Instructions: json.RawMessage(`""`)},
				},
			},
			wantErr: "must have criteria or instructions",
		},
		{
			name: "choice without criteria",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"department": {Type: TypeSafeQuestionChoice, Instructions: json.RawMessage(`"which team?"`)},
				},
			},
			wantErr: "choice criteria must be an object",
		},
		{
			name: "choice with empty criteria",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"department": {
						Type:         TypeSafeQuestionChoice,
						Instructions: json.RawMessage(`"which team?"`),
						Criteria:     json.RawMessage(`{}`),
					},
				},
			},
			wantErr: "at least one option",
		},
		{
			name: "choice criteria accepts null descriptions",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"department": {
						Type:         TypeSafeQuestionChoice,
						Instructions: json.RawMessage(`"which team?"`),
						Criteria:     json.RawMessage(`{"billing":"payments","technical":null}`),
					},
				},
			},
		},
		{
			// The upstream answers a single-level score question even though the
			// docs ask for two levels; only an empty list is rejected.
			name: "score with a single level",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"frustration": {
						Type:         TypeSafeQuestionScore,
						Instructions: json.RawMessage(`"how frustrated?"`),
						Criteria:     json.RawMessage(`["Calm"]`),
					},
				},
			},
		},
		{
			name: "score with empty criteria",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"frustration": {
						Type:         TypeSafeQuestionScore,
						Instructions: json.RawMessage(`"how frustrated?"`),
						Criteria:     json.RawMessage(`[]`),
					},
				},
			},
			wantErr: "at least one level",
		},
		{
			name: "score criteria must be an array",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"frustration": {
						Type:         TypeSafeQuestionScore,
						Instructions: json.RawMessage(`"how frustrated?"`),
						Criteria:     json.RawMessage(`{"0":"Calm","1":"Angry"}`),
					},
				},
			},
			wantErr: "score criteria must be an array",
		},
		{
			name: "score with two levels",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"frustration": {
						Type:         TypeSafeQuestionScore,
						Instructions: json.RawMessage(`"how frustrated?"`),
						Criteria:     json.RawMessage(`["Calm","Very angry"]`),
					},
				},
			},
		},
		// instructions is optional for every type as long as criteria carries the
		// question; the upstream answers all three of these.
		{
			name: "noul carried by criteria alone",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"is_urgent": {
						Type:     TypeSafeQuestionNoul,
						Criteria: json.RawMessage(`{"true":"is urgent","false":"not urgent"}`),
					},
				},
			},
		},
		{
			name: "choice carried by criteria alone",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"department": {
						Type:     TypeSafeQuestionChoice,
						Criteria: json.RawMessage(`{"billing":"money","technical":"bugs"}`),
					},
				},
			},
		},
		{
			name: "score carried by criteria alone",
			request: TypeSafeRequest{
				Model:     "jev-latest",
				State:     json.RawMessage(`"payouts failing"`),
				Questions: map[string]TypeSafeQuestion{
					"frustration": {
						Type:     TypeSafeQuestionScore,
						Criteria: json.RawMessage(`["Calm","Angry"]`),
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.request.Validate()
			if testCase.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

// Question and criteria counts decide how much text a single request bills, so
// they must be rejected at validation rather than silently inflating the prompt.
func TestTypeSafeRequestValidateBoundsFanOut(t *testing.T) {
	request := TypeSafeRequest{
		Model:     "jev-latest",
		State:     json.RawMessage(`"payouts failing"`),
		Questions: make(map[string]TypeSafeQuestion, MaxTypeSafeQuestions+1),
	}
	for i := 0; i <= MaxTypeSafeQuestions; i++ {
		request.Questions[fmt.Sprintf("q%d", i)] = TypeSafeQuestion{
			Type:         TypeSafeQuestionNoul,
			Instructions: json.RawMessage(`"urgent?"`),
		}
	}
	err := request.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("at most %d entries", MaxTypeSafeQuestions))

	options := make([]string, 0, MaxTypeSafeCriteria+1)
	for i := 0; i <= MaxTypeSafeCriteria; i++ {
		options = append(options, fmt.Sprintf(`"o%d":"desc"`, i))
	}
	oversizedChoice := TypeSafeRequest{
		Model: "jev-latest",
		State: json.RawMessage(`"payouts failing"`),
		Questions: map[string]TypeSafeQuestion{
			"department": {
				Type:         TypeSafeQuestionChoice,
				Instructions: json.RawMessage(`"which team?"`),
				Criteria:     json.RawMessage(`{` + strings.Join(options, ",") + `}`),
			},
		},
	}
	err = oversizedChoice.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("at most %d options", MaxTypeSafeCriteria))
}

// Question ids are documented as not reaching the model, so billing must not
// count them; every field that does reach the model must be counted.
func TestTypeSafeRequestTokenCountMeta(t *testing.T) {
	request := TypeSafeRequest{
		Model: "jev-latest",
		State: json.RawMessage(`"payouts have been failing"`),
		Questions: map[string]TypeSafeQuestion{
			"a_unique_question_id": {
				Type:         TypeSafeQuestionScore,
				Instructions: json.RawMessage(`"how frustrated?"`),
				Criteria:     json.RawMessage(`["Calm","Very angry"]`),
			},
			"b_second_question": {
				Type:         TypeSafeQuestionNoul,
				Instructions: json.RawMessage(`"does this convey urgency?"`),
			},
		},
	}

	meta := request.GetTokenCountMeta()

	assert.Equal(t, 2, meta.MessagesCount)
	assert.NotContains(t, meta.CombineText, "a_unique_question_id")
	assert.NotContains(t, meta.CombineText, "b_second_question")
	// A plain string state is counted unquoted; structured criteria are counted
	// as the JSON the model receives.
	assert.Equal(t, strings.Join([]string{
		"payouts have been failing",
		"how frustrated?",
		`["Calm","Very angry"]`,
		"does this convey urgency?",
	}, "\n"), meta.CombineText)
}
