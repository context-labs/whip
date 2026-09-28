package protocol

import (
	"encoding/json"
	"strconv"
)

// CursorMap encodes durable counters without JavaScript's integer rounding.
type CursorMap map[string]int64

func (c CursorMap) MarshalJSON() ([]byte, error) {
	values := make(map[string]string, len(c))
	for key, value := range c {
		values[key] = strconv.FormatInt(value, 10)
	}
	return json.Marshal(values)
}

func (c *CursorMap) UnmarshalJSON(data []byte) error {
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	result := make(CursorMap, len(values))
	for key, value := range values {
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		result[key] = number
	}
	*c = result
	return nil
}
