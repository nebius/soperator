package slurmapi

import "encoding/json"

// optionalNumber honors Slurm's set/infinite flags before decoding number.
// Unset and infinite values may carry unsigned sentinels that do not fit the
// signed types used for resource quantities and Unix timestamps.
type optionalNumber[T int32 | int64] struct {
	Set      *bool `json:"set,omitempty"`
	Infinite *bool `json:"infinite,omitempty"`
	Number   *T    `json:"number,omitempty"`
}

func (n *optionalNumber[T]) UnmarshalJSON(data []byte) error {
	var raw struct {
		Set      *bool           `json:"set"`
		Infinite *bool           `json:"infinite"`
		Number   json.RawMessage `json:"number"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*n = optionalNumber[T]{Set: raw.Set, Infinite: raw.Infinite}
	if raw.Set == nil || !*raw.Set || (raw.Infinite != nil && *raw.Infinite) || len(raw.Number) == 0 {
		return nil
	}
	return json.Unmarshal(raw.Number, &n.Number)
}
