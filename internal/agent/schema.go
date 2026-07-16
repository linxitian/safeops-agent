package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
)

func validateToolArguments(raw json.RawMessage, arguments map[string]any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return errors.New("tool has no discovered input schema")
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	return validateSchemaValue(schema, arguments, "$", true)
}

func validateSchemaValue(schema map[string]any, value any, path string, root bool) error {
	typeName, _ := schema["type"].(string)
	if root && typeName == "" {
		typeName = "object"
	}
	switch typeName {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		required := map[string]bool{}
		if values, ok := schema["required"].([]any); ok {
			for _, item := range values {
				if name, ok := item.(string); ok {
					required[name] = true
				}
			}
		}
		for name := range required {
			if _, exists := object[name]; !exists {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
		additional, hasAdditional := schema["additionalProperties"].(bool)
		for name, child := range object {
			childSchemaValue, exists := properties[name]
			if !exists {
				if hasAdditional && !additional {
					return fmt.Errorf("%s.%s is not allowed", path, name)
				}
				continue
			}
			childSchema, ok := childSchemaValue.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.%s has an invalid discovered schema", path, name)
			}
			if err := validateSchemaValue(childSchema, child, path+"."+name, false); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if pattern, ok := schema["pattern"].(string); ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("%s has invalid discovered pattern", path)
			}
			if !re.MatchString(text) {
				return fmt.Errorf("%s does not match the required pattern", path)
			}
		}
		if err := validateEnum(schema, value, path); err != nil {
			return err
		}
	case "integer":
		number, ok := numeric(value)
		if !ok || math.Trunc(number) != number {
			return fmt.Errorf("%s must be an integer", path)
		}
		if err := validateNumberBounds(schema, number, path); err != nil {
			return err
		}
	case "number":
		number, ok := numeric(value)
		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}
		if err := validateNumberBounds(schema, number, path); err != nil {
			return err
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "array":
		values, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for index, item := range values {
				if err := validateSchemaValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, index), false); err != nil {
					return err
				}
			}
		}
	case "", "null":
		// Some compatible servers expose unconstrained optional fields.
	default:
		return fmt.Errorf("%s uses unsupported discovered schema type %q", path, typeName)
	}
	return nil
}

func numeric(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		number, err := value.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}

func validateNumberBounds(schema map[string]any, number float64, path string) error {
	if minimum, ok := numeric(schema["minimum"]); ok && number < minimum {
		return fmt.Errorf("%s is below minimum %v", path, minimum)
	}
	if maximum, ok := numeric(schema["maximum"]); ok && number > maximum {
		return fmt.Errorf("%s exceeds maximum %v", path, maximum)
	}
	return nil
}

func validateEnum(schema map[string]any, value any, path string) error {
	items, ok := schema["enum"].([]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		if fmt.Sprint(item) == fmt.Sprint(value) {
			return nil
		}
	}
	return fmt.Errorf("%s is outside the allowed enum", path)
}
