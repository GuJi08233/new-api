package model

import (
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONColumnStringAndBytesRoundTrip(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		value      driver.Valuer
		newScanner func() sql.Scanner
	}{
		{"channel", ChannelInfo{IsMultiKey: true, MultiKeySize: 2}, func() sql.Scanner { return &ChannelInfo{} }},
		{"properties", Properties{Input: "hello"}, func() sql.Scanner { return &Properties{} }},
		{"private", TaskPrivateData{Key: "secret"}, func() sql.Scanner { return &TaskPrivateData{} }},
		{"prefill", JSONValue(`[{"name":"model"}]`), func() sql.Scanner { return new(JSONValue) }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			value, err := fixture.value.Value()
			require.NoError(t, err)
			text, ok := value.(string)
			require.True(t, ok, "PG simple protocol must receive text, not bytea")
			for _, input := range []any{text, []byte(text)} {
				scanner := fixture.newScanner()
				require.NoError(t, scanner.Scan(input))
				encoded, err := scanner.(driver.Valuer).Value()
				require.NoError(t, err)
				assert.JSONEq(t, text, encoded.(string))
				require.NoError(t, scanner.Scan(nil))
				zero, err := scanner.(driver.Valuer).Value()
				require.NoError(t, err)
				if fixture.name != "channel" {
					assert.Nil(t, zero)
				}
			}
		})
	}
}
