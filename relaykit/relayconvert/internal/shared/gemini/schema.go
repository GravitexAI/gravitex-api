package gemini

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

var geminiOpenAPISchemaAllowedFields = map[string]struct{}{
	"anyOf":            {},
	"default":          {},
	"description":      {},
	"enum":             {},
	"example":          {},
	"format":           {},
	"items":            {},
	"maxItems":         {},
	"maxLength":        {},
	"maxProperties":    {},
	"maximum":          {},
	"minItems":         {},
	"minLength":        {},
	"minProperties":    {},
	"minimum":          {},
	"nullable":         {},
	"pattern":          {},
	"properties":       {},
	"propertyOrdering": {},
	"required":         {},
	"title":            {},
	"type":             {},
}

const geminiFunctionSchemaMaxDepth = 64

func CleanFunctionParameters(params interface{}) interface{} {
	return cleanGeminiFunctionParametersWithDepth(params, 0)
}

func cleanGeminiFunctionParametersWithDepth(params interface{}, depth int) interface{} {
	if params == nil {
		return nil
	}

	if depth >= geminiFunctionSchemaMaxDepth {
		return cleanGeminiFunctionParametersShallow(params)
	}

	switch v := params.(type) {
	case map[string]interface{}:
		cleanedMap := make(map[string]interface{}, len(v))
		for key, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[key]; ok {
				cleanedMap[key] = val
			}
		}

		normalizeGeminiSchemaTypeAndNullable(cleanedMap)
		normalizeGeminiSchemaEnum(cleanedMap)

		if props, ok := cleanedMap["properties"].(map[string]interface{}); ok && props != nil {
			cleanedProps := make(map[string]interface{})
			for propName, propValue := range props {
				cleanedProps[propName] = cleanGeminiFunctionParametersWithDepth(propValue, depth+1)
			}
			cleanedMap["properties"] = cleanedProps
		}

		if items, ok := cleanedMap["items"].(map[string]interface{}); ok && items != nil {
			cleanedMap["items"] = cleanGeminiFunctionParametersWithDepth(items, depth+1)
		}
		if itemsArray, ok := cleanedMap["items"].([]interface{}); ok && len(itemsArray) > 0 {
			cleanedMap["items"] = cleanGeminiFunctionParametersWithDepth(itemsArray[0], depth+1)
		}

		if nested, ok := cleanedMap["anyOf"].([]interface{}); ok && nested != nil {
			cleanedNested := make([]interface{}, 0, len(nested))
			for _, item := range nested {
				cleaned := cleanGeminiFunctionParametersWithDepth(item, depth+1)
				// A JSON Schema {"type":"null"} branch normalizes to a bare nullable marker.
				// Gemini has no null type, so such a branch would reach the API carrying no
				// type at all and make it reject the whole declaration with "didn't specify
				// the schema type field"; the nullability belongs on the parent instead.
				if branch, isMap := cleaned.(map[string]interface{}); isMap {
					nullable, _ := branch["nullable"].(bool)
					_, hasType := branch["type"]
					_, hasAnyOf := branch["anyOf"]
					_, hasProperties := branch["properties"]
					_, hasItems := branch["items"]
					if nullable && !hasType && !hasAnyOf && !hasProperties && !hasItems {
						cleanedMap["nullable"] = true
						continue
					}
				}
				cleanedNested = append(cleanedNested, cleaned)
			}

			branch, singleIsMap := map[string]interface{}(nil), false
			if len(cleanedNested) == 1 {
				branch, singleIsMap = cleanedNested[0].(map[string]interface{})
			}
			_, parentHasType := cleanedMap["type"]
			switch {
			case len(cleanedNested) == 0:
				delete(cleanedMap, "anyOf")
			case singleIsMap && !parentHasType:
				// The sole remaining branch carries the type this node is missing, so fold it
				// in rather than leaving a one-way anyOf around a typeless parent.
				delete(cleanedMap, "anyOf")
				for key, val := range branch {
					if _, exists := cleanedMap[key]; !exists {
						cleanedMap[key] = val
					}
				}
			default:
				cleanedMap["anyOf"] = cleanedNested
			}
		}

		return cleanedMap
	case []interface{}:
		cleanedArray := make([]interface{}, len(v))
		for i, item := range v {
			cleanedArray[i] = cleanGeminiFunctionParametersWithDepth(item, depth+1)
		}
		return cleanedArray
	default:
		return params
	}
}

func cleanGeminiFunctionParametersShallow(params interface{}) interface{} {
	switch v := params.(type) {
	case map[string]interface{}:
		cleanedMap := make(map[string]interface{}, len(v))
		for key, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[key]; ok {
				cleanedMap[key] = val
			}
		}
		normalizeGeminiSchemaTypeAndNullable(cleanedMap)
		normalizeGeminiSchemaEnum(cleanedMap)
		delete(cleanedMap, "properties")
		delete(cleanedMap, "items")
		delete(cleanedMap, "anyOf")
		return cleanedMap
	case []interface{}:
		return []interface{}{}
	default:
		return params
	}
}

// normalizeGeminiSchemaEnum rewrites enum members into the repeated-string form Gemini's
// Schema requires. Google's function-calling docs keep the declared type and ask for
// "integer (or any other type) values as strings", so only the members are rewritten.
// Without this, a tool declaring {"type":"integer","enum":[10,20]} makes the upstream
// reject the whole request with "Invalid value at '...enum[0]' (TYPE_STRING)".
func normalizeGeminiSchemaEnum(schema map[string]interface{}) {
	raw, ok := schema["enum"]
	if !ok {
		return
	}

	members, ok := raw.([]interface{})
	if !ok {
		delete(schema, "enum")
		return
	}

	normalized := make([]interface{}, 0, len(members))
	for _, member := range members {
		switch v := member.(type) {
		case string:
			normalized = append(normalized, v)
		case bool:
			normalized = append(normalized, strconv.FormatBool(v))
		case float64:
			normalized = append(normalized, strconv.FormatFloat(v, 'f', -1, 64))
		case nil:
			// JSON Schema spells an optional value as a null enum member; Gemini has no such
			// enum member and expresses the same thing with the nullable flag.
			schema["nullable"] = true
		}
		// Objects and arrays have no Gemini enum representation, so they are dropped.
	}

	if len(normalized) == 0 {
		delete(schema, "enum")
		return
	}
	schema["enum"] = normalized
}

func normalizeGeminiSchemaTypeAndNullable(schema map[string]interface{}) {
	rawType, ok := schema["type"]
	if !ok || rawType == nil {
		return
	}

	normalize := func(t string) (string, bool) {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "object":
			return "OBJECT", false
		case "array":
			return "ARRAY", false
		case "string":
			return "STRING", false
		case "integer":
			return "INTEGER", false
		case "number":
			return "NUMBER", false
		case "boolean":
			return "BOOLEAN", false
		case "null":
			return "", true
		default:
			return t, false
		}
	}

	switch typed := rawType.(type) {
	case string:
		normalized, isNull := normalize(typed)
		if isNull {
			schema["nullable"] = true
			delete(schema, "type")
			return
		}
		schema["type"] = normalized
	case []interface{}:
		nullable := false
		var chosen string
		for _, item := range typed {
			if value, ok := item.(string); ok {
				normalized, isNull := normalize(value)
				if isNull {
					nullable = true
					continue
				}
				if chosen == "" {
					chosen = normalized
				}
			}
		}
		if nullable {
			schema["nullable"] = true
		}
		if chosen != "" {
			schema["type"] = chosen
		} else {
			delete(schema, "type")
		}
	}
}

func RemoveAdditionalProperties(schema interface{}, depth int) interface{} {
	if depth >= 5 {
		return schema
	}

	value, ok := schema.(map[string]interface{})
	if !ok || len(value) == 0 {
		return schema
	}
	delete(value, "title")
	delete(value, "$schema")
	if typeVal, exists := value["type"]; !exists || (typeVal != "object" && typeVal != "array") {
		return schema
	}
	switch value["type"] {
	case "object":
		delete(value, "additionalProperties")
		if properties, ok := value["properties"].(map[string]interface{}); ok {
			for key, nested := range properties {
				properties[key] = RemoveAdditionalProperties(nested, depth+1)
			}
		}
		for _, field := range []string{"allOf", "anyOf", "oneOf"} {
			if nested, ok := value[field].([]interface{}); ok {
				for i, item := range nested {
					nested[i] = RemoveAdditionalProperties(item, depth+1)
				}
			}
		}
	case "array":
		if items, ok := value["items"].(map[string]interface{}); ok {
			value["items"] = RemoveAdditionalProperties(items, depth+1)
		}
	}

	return value
}

func OpenAIToolChoiceToConfig(toolChoice any) *dto.ToolConfig {
	if toolChoice == nil {
		return nil
	}

	if toolChoiceStr, ok := toolChoice.(string); ok {
		config := &dto.ToolConfig{
			FunctionCallingConfig: &dto.FunctionCallingConfig{},
		}
		switch toolChoiceStr {
		case "auto":
			config.FunctionCallingConfig.Mode = "AUTO"
		case "none":
			config.FunctionCallingConfig.Mode = "NONE"
		case "required":
			config.FunctionCallingConfig.Mode = "ANY"
		default:
			config.FunctionCallingConfig.Mode = "AUTO"
		}
		return config
	}

	if toolChoiceMap, ok := toolChoice.(map[string]interface{}); ok {
		if toolChoiceMap["type"] == "function" {
			config := &dto.ToolConfig{
				FunctionCallingConfig: &dto.FunctionCallingConfig{
					Mode: "ANY",
				},
			}
			if function, ok := toolChoiceMap["function"].(map[string]interface{}); ok {
				if name, ok := function["name"].(string); ok && name != "" {
					config.FunctionCallingConfig.AllowedFunctionNames = []string{name}
				}
			}
			return config
		}
		return nil
	}

	return nil
}
