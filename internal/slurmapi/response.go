package slurmapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
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
	if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		return nil, fmt.Errorf("decode Slurm response: unexpected content type %q", resp.Header.Get("Content-Type"))
	}
	var envelope struct {
		Errors api.V0041OpenapiErrors `json:"errors"`
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

// summarizeSlurmRESTBody extracts the descriptions/errors from a Slurm REST API JSON envelope,
// which all error responses follow ({"errors": [{"description": "...", "error": "..."}], ...}).
// Returns "errors=[...]" when the envelope parses, otherwise the raw body. Used to keep error
// logs scannable instead of dumping kilobytes of envelope metadata.
func summarizeSlurmRESTBody(body []byte) string {
	var parsed struct {
		Errors []struct {
			Description string `json:"description"`
			Error       string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && len(parsed.Errors) > 0 {
		msgs := make([]string, 0, len(parsed.Errors))
		for _, e := range parsed.Errors {
			switch {
			case e.Description != "":
				msgs = append(msgs, e.Description)
			case e.Error != "":
				msgs = append(msgs, e.Error)
			}
		}
		if len(msgs) > 0 {
			return fmt.Sprintf("errors=%v", msgs)
		}
	}
	return fmt.Sprintf("body=%s", string(body))
}
