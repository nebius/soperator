package slurmapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
)

// UndrainNode moves a Slurm node out of DRAIN state through slurmrestd.
func (c *client) UndrainNode(ctx context.Context, nodeName string) error {
	if nodeName == "" {
		return fmt.Errorf("undrain node: node name is required")
	}

	states := []api.V0044UpdateNodeMsgState{api.V0044UpdateNodeMsgStateUNDRAIN}
	response, err := c.SlurmV0044PostNodeWithResponse(ctx, nodeName, api.V0044UpdateNodeMsg{
		State: &states,
	})
	if err != nil {
		return fmt.Errorf("post undrain node %s request: %w", nodeName, err)
	}
	if response.StatusCode() < http.StatusOK || response.StatusCode() >= http.StatusMultipleChoices {
		return fmt.Errorf(
			"undrain node %s request failed: status=%d %s",
			nodeName,
			response.StatusCode(),
			summarizeSlurmRESTBody(response.Body),
		)
	}
	if len(bytes.TrimSpace(response.Body)) == 0 {
		return nil
	}

	var responseEnvelope api.V0044OpenapiResp
	if err := json.Unmarshal(response.Body, &responseEnvelope); err != nil {
		return fmt.Errorf("decode undrain node %s response: %w", nodeName, err)
	}
	if responseEnvelope.Errors != nil && len(*responseEnvelope.Errors) > 0 {
		return fmt.Errorf(
			"undrain node %s responded with errors: %s",
			nodeName,
			summarizeSlurmRESTBody(response.Body),
		)
	}

	return nil
}
