package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiFunctionPreservesComplexJSONSchema(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"value":{"const":42}},"required":["value"]}`,
		`{"type":"object","properties":{"value":{"oneOf":[{"type":"string"},{"type":"integer"}]}}}`,
		`{"$defs":{"value":{"enum":[1,2]}},"type":"object","properties":{"value":{"$ref":"#/$defs/value"}}}`,
		`{"type":"object","properties":{"value":{"type":["string","null"]}}}`,
	} {
		t.Run(schema, func(t *testing.T) {
			var parameters any
			require.NoError(t, common.UnmarshalJsonStr(schema, &parameters))
			declaration := FunctionDeclaration(dto.FunctionRequest{Name: "test", Parameters: parameters}, true)
			assert.Nil(t, declaration.Parameters)
			encoded, err := common.Marshal(declaration.ParametersJsonSchema)
			require.NoError(t, err)
			assert.JSONEq(t, schema, string(encoded))
		})
	}
}

func TestGeminiOrdinaryToolsAndV1KeepTypedParameters(t *testing.T) {
	var parameters any
	require.NoError(t, common.UnmarshalJsonStr(`{"type":"object","properties":{"value":{"type":"string"}},"additionalProperties":false}`, &parameters))
	declaration := FunctionDeclaration(dto.FunctionRequest{Name: "test", Parameters: parameters}, true)
	assert.Nil(t, declaration.ParametersJsonSchema)
	require.NotNil(t, declaration.Parameters)
	var complex any
	require.NoError(t, common.UnmarshalJsonStr(`{"type":"object","properties":{"value":{"type":"integer","const":42}}}`, &complex))
	declaration = FunctionDeclaration(dto.FunctionRequest{Name: "test", Parameters: complex}, false)
	assert.Nil(t, declaration.ParametersJsonSchema)
	require.NotNil(t, declaration.Parameters)
}

func TestGeminiStructuredOutputPreservesRawSchema(t *testing.T) {
	format := []byte(`{"name":"answer","schema":{"type":"object","properties":{"v":{"enum":[1,2]}}}}`)
	config := &dto.GeminiChatGenerationConfig{}
	require.NoError(t, ApplyResponseSchema(config, format, true))
	assert.Nil(t, config.ResponseSchema)
	assert.JSONEq(t, `{"type":"object","properties":{"v":{"enum":[1,2]}}}`, string(config.ResponseJsonSchema))
}
