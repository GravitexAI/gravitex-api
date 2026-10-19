package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Gemini's Schema.enum is `repeated string`, so any non-string enum element makes
// Vertex reject the whole request with
// "Invalid value at 'tools[0].function_declarations[N].parameters.properties[0].value.enum[0]' (TYPE_STRING)".
// Google's own docs require integer (or any other type) enum values to be sent as strings
// while keeping the declared type, so the cleaner must stringify them instead of dropping them.
func TestCleanFunctionParametersProducesGeminiCompatibleSchema(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "integer enum keeps type and stringifies values",
			input: `{"type":"object","properties":{"status":{"type":"integer","enum":[10,20,30]}}}`,
			want:  `{"properties":{"status":{"enum":["10","20","30"],"type":"INTEGER"}},"type":"OBJECT"}`,
		},
		{
			name:  "boolean enum",
			input: `{"type":"object","properties":{"flag":{"type":"boolean","enum":[true,false]}}}`,
			want:  `{"properties":{"flag":{"enum":["true","false"],"type":"BOOLEAN"}},"type":"OBJECT"}`,
		},
		{
			name:  "number enum keeps fractional values readable",
			input: `{"type":"object","properties":{"ratio":{"type":"number","enum":[1,1.5,-0.25]}}}`,
			want:  `{"properties":{"ratio":{"enum":["1","1.5","-0.25"],"type":"NUMBER"}},"type":"OBJECT"}`,
		},
		{
			name:  "null element becomes nullable instead of an enum value",
			input: `{"type":"object","properties":{"mode":{"type":"string","enum":["a",null]}}}`,
			want:  `{"properties":{"mode":{"enum":["a"],"nullable":true,"type":"STRING"}},"type":"OBJECT"}`,
		},
		{
			name:  "enum with only null drops the enum key",
			input: `{"type":"object","properties":{"mode":{"type":"string","enum":[null]}}}`,
			want:  `{"properties":{"mode":{"nullable":true,"type":"STRING"}},"type":"OBJECT"}`,
		},
		{
			name:  "enum elements that cannot be a Gemini enum value are dropped",
			input: `{"type":"object","properties":{"mode":{"type":"string","enum":["a",{"k":"v"},["x"]]}}}`,
			want:  `{"properties":{"mode":{"enum":["a"],"type":"STRING"}},"type":"OBJECT"}`,
		},
		{
			name:  "enum that is not an array is dropped",
			input: `{"type":"object","properties":{"mode":{"type":"string","enum":"a"}}}`,
			want:  `{"properties":{"mode":{"type":"STRING"}},"type":"OBJECT"}`,
		},
		{
			name:  "enum inside array items",
			input: `{"type":"object","properties":{"codes":{"type":"array","items":{"type":"integer","enum":[1,2]}}}}`,
			want:  `{"properties":{"codes":{"items":{"enum":["1","2"],"type":"INTEGER"},"type":"ARRAY"}},"type":"OBJECT"}`,
		},
		{
			name:  "enum inside anyOf branches",
			input: `{"type":"object","properties":{"v":{"anyOf":[{"type":"integer","enum":[7]},{"type":"string","enum":["x"]}]}}}`,
			want:  `{"properties":{"v":{"anyOf":[{"enum":["7"],"type":"INTEGER"},{"enum":["x"],"type":"STRING"}]}},"type":"OBJECT"}`,
		},
		{
			name:  "null branch folds into the parent as nullable",
			input: `{"type":"object","properties":{"sectionId":{"anyOf":[{"type":"string","description":"Section id"},{"type":"null"}],"description":"Destination section id."}}}`,
			want:  `{"properties":{"sectionId":{"description":"Destination section id.","nullable":true,"type":"STRING"}},"type":"OBJECT"}`,
		},
		{
			name:  "null branch folds in while several real branches stay",
			input: `{"type":"object","properties":{"target":{"anyOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"string"}}},{"type":"null"}]}}}`,
			want:  `{"properties":{"target":{"anyOf":[{"properties":{"a":{"type":"STRING"}},"type":"OBJECT"},{"properties":{"b":{"type":"STRING"}},"type":"OBJECT"}],"nullable":true}},"type":"OBJECT"}`,
		},
		{
			name:  "collapsing a single branch keeps the parent description",
			input: `{"type":"object","properties":{"p":{"anyOf":[{"type":"integer","description":"inner"}],"description":"outer"}}}`,
			want:  `{"properties":{"p":{"description":"outer","type":"INTEGER"}},"type":"OBJECT"}`,
		},
		{
			name:  "nested anyOf null branch is folded at every depth",
			input: `{"type":"object","properties":{"t":{"anyOf":[{"type":"object","properties":{"env":{"anyOf":[{"type":"string"},{"type":"null"}]}}},{"type":"null"}]}}}`,
			want:  `{"properties":{"t":{"nullable":true,"properties":{"env":{"nullable":true,"type":"STRING"}},"type":"OBJECT"}},"type":"OBJECT"}`,
		},
		{
			name:  "a typeless branch that is not a null marker is kept",
			input: `{"type":"object","properties":{"p":{"anyOf":[{"type":"string"},{"properties":{"a":{"type":"string"}}}]}}}`,
			want:  `{"properties":{"p":{"anyOf":[{"type":"STRING"},{"properties":{"a":{"type":"STRING"}}}]}},"type":"OBJECT"}`,
		},
		{
			name:  "string enum is left untouched",
			input: `{"type":"object","properties":{"unit":{"type":"string","enum":["celsius","fahrenheit"]}}}`,
			want:  `{"properties":{"unit":{"enum":["celsius","fahrenheit"],"type":"STRING"}},"type":"OBJECT"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var params map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(tc.input), &params))

			got, err := json.Marshal(CleanFunctionParameters(params))
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(got))
		})
	}
}
