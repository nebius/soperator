package slurmapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
)

func decodeResponse[T any](resp *http.Response) (*T, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read Slurm response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status=%d %s", resp.StatusCode, summarizeSlurmRESTBody(body))
	}
	var envelope struct {
		Errors api.V0044OpenapiErrors `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode Slurm response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return nil, fmt.Errorf("Slurm response: %s", summarizeSlurmRESTBody(body))
	}
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode Slurm response: %w", err)
	}
	return &result, nil
}
