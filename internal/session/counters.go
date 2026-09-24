package session

import (
	"encoding/json"
	"strconv"
)

// DecimalCounters is used by lifecycle notifications containing inbox counters.
type DecimalCounters []int64

func (c DecimalCounters) MarshalJSON() ([]byte, error) {
	values := make([]string, len(c))
	for index, value := range c {
		values[index] = strconv.FormatInt(value, 10)
	}
	return json.Marshal(values)
}

func (c *DecimalCounters) UnmarshalJSON(data []byte) error {
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	result := make(DecimalCounters, len(values))
	for index, value := range values {
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		result[index] = number
	}
	*c = result
	return nil
}
