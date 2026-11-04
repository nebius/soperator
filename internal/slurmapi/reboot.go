package slurmapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
)

const slurmV0044RebootNodesPath = "/slurm/v0.0.44/nodes/reboot"

// RebootNextState is the state Slurm assigns to a node after rebooting it.
type RebootNextState string

const (
	// RebootNextStateDown leaves the node in DOWN state after the reboot.
	RebootNextStateDown RebootNextState = "DOWN"
	// RebootNextStateResume resumes the node after the reboot.
	RebootNextStateResume RebootNextState = "RESUME"
)

// RebootNodesRequest contains the options supported by scontrol reboot.
type RebootNodesRequest struct {
	NodeList    string            `json:"nodes"`
	ASAP        bool              `json:"asap,omitempty"`
	Force       bool              `json:"force,omitempty"`
	NextState   []RebootNextState `json:"next_state,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	PowerAction string            `json:"power_action,omitempty"`
}

// RebootNodes schedules a reboot for a Slurm node list through slurmrestd.
func (c *client) RebootNodes(ctx context.Context, request RebootNodesRequest) error {
	if request.NodeList == "" {
		return fmt.Errorf("reboot nodes: nodes field is required")
	}

	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal reboot nodes request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.server+slurmV0044RebootNodesPath,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create reboot nodes request: %w", err)
	}
	if err := c.setHeaders(ctx, httpRequest); err != nil {
		return fmt.Errorf("set reboot nodes request headers: %w", err)
	}

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("post reboot nodes request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read reboot nodes response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf(
			"reboot nodes request failed: status=%d %s",
			response.StatusCode,
			summarizeSlurmRESTBody(responseBody),
		)
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		return nil
	}

	var responseEnvelope api.V0044OpenapiResp
	if err := json.Unmarshal(responseBody, &responseEnvelope); err != nil {
		return fmt.Errorf("decode reboot nodes response: %w", err)
	}
	if responseEnvelope.Errors != nil && len(*responseEnvelope.Errors) > 0 {
		return fmt.Errorf("reboot nodes responded with errors: %s", summarizeSlurmRESTBody(responseBody))
	}

	return nil
}
