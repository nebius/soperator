package slurmapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
)

// UndrainNodes moves a batch of Slurm nodes out of DRAIN state through slurmrestd.
// An error may indicate partial application; callers must re-read state before retrying.
func (c *client) UndrainNodes(ctx context.Context, nodeNames []string) error {
	if len(nodeNames) == 0 {
		return fmt.Errorf("undrain nodes: node names are required")
	}
	for _, name := range nodeNames {
		if name == "" {
			return fmt.Errorf("undrain nodes: node name is empty")
		}
	}

	states := []api.V0044UpdateNodeMsgState{api.V0044UpdateNodeMsgStateUNDRAIN}
	response, err := c.SlurmV0044PostNodesWithResponse(ctx, api.V0044UpdateNodeMsg{
		Name:  &nodeNames,
		State: &states,
	})
	if err != nil {
		return fmt.Errorf("post undrain nodes request: %w", err)
	}
	if response.StatusCode() < http.StatusOK || response.StatusCode() >= http.StatusMultipleChoices {
		return fmt.Errorf(
			"undrain nodes: status=%d %s",
			response.StatusCode(),
			summarizeSlurmRESTBody(response.Body),
		)
	}
	if len(bytes.TrimSpace(response.Body)) == 0 {
		return nil
	}

	var responseEnvelope api.V0044OpenapiResp
	if err := json.Unmarshal(response.Body, &responseEnvelope); err != nil {
		return fmt.Errorf("decode undrain nodes response: %w", err)
	}
	if responseEnvelope.Errors != nil && len(*responseEnvelope.Errors) > 0 {
		return fmt.Errorf(
			"undrain nodes responded with errors: %s",
			summarizeSlurmRESTBody(response.Body),
		)
	}

	return nil
}
