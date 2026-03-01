package trader

import (
	"fmt"
	"strconv"
)

// SafeFloat64 Safely extract float64 value from map (nil/missing => 0)
func SafeFloat64(data map[string]interface{}, key string) (float64, error) {
	value, ok := data[key]
	if !ok || value == nil {
		return 0, nil
	}

	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		// Try to parse string as float64
		if v == "" {
			return 0, nil
		}
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse string '%s' as float64: %w", v, err)
		}
		return parsed, nil
	default:
		// nil interface or other types: treat as 0 to avoid panic
		return 0, nil
	}
}

// SafeString Safely extract string value from map
func SafeString(data map[string]interface{}, key string) (string, error) {
	value, ok := data[key]
	if !ok {
		return "", fmt.Errorf("key '%s' not found", key)
	}

	switch v := value.(type) {
	case string:
		return v, nil
	case fmt.Stringer:
		return v.String(), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// SafeInt Safely extract int value from map
func SafeInt(data map[string]interface{}, key string) (int, error) {
	value, ok := data[key]
	if !ok {
		return 0, fmt.Errorf("key '%s' not found", key)
	}

	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("cannot parse string '%s' as int: %w", v, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("value for key '%s' is not an integer (type: %T)", key, v)
	}
}
