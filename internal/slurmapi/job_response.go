package slurmapi

import (
	"encoding/json"
	"net/http"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
)

// Slurm's TOP priority and symbolic CPU frequencies exceed the signed int32
// fields in the generated bindings. These fields are not used by Job conversion.
type controllerJob struct {
	api.V0041JobInfo
	Priority             json.RawMessage `json:"priority"`
	CPUFrequencyMinimum  json.RawMessage `json:"cpu_frequency_minimum"`
	CPUFrequencyMaximum  json.RawMessage `json:"cpu_frequency_maximum"`
	CPUFrequencyGovernor json.RawMessage `json:"cpu_frequency_governor"`
}

type jobsResponse[T any] struct {
	Jobs []T `json:"jobs"`
}

func decodeJobsResponse[T any](resp *http.Response) ([]T, error) {
	result, err := decodeResponse[jobsResponse[T]](resp)
	if err != nil {
		return nil, err
	}
	return result.Jobs, nil
}
