package slurmapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
)

// ErrTopologyUnavailable reports that slurmctld does not know one of the topologies named in the
// registration.
//
// It means the running slurmctld has not loaded the topology yet, not that the request was wrong:
// topology_g_init parses topology.yaml once per process, so a newly declared topology only exists
// after a reconfigure re-execs the controller. Slurm rolls the node's previous registration back
// before answering, so a caller can simply retry once the reconfigure lands.
var ErrTopologyUnavailable = errors.New("requested topology is not loaded by slurmctld")

// slurmErrTopologyUnavailable is the short form Slurm reports for
// ESLURM_REQUESTED_TOPO_CONFIG_UNAVAILABLE (2178). Matched by name rather than by number because
// the numeric values shift whenever an entry is inserted into the errno enum.
const slurmErrTopologyUnavailable = "ESLURM_REQUESTED_TOPO_CONFIG_UNAVAILABLE"

// SetNodeTopology registers nodes into the topologies named by topologyStr.
//
// nodes is a Slurm hostlist expression, so one call covers every node sharing a registration --
// which is every node of one leaf switch or one block. slurmrestd passes the path segment straight
// into the update request, and slurmctld expands it, so the cost to the controller is one RPC per
// distinct registration rather than one per node.
//
// The registration is exhaustive: slurmctld removes the nodes from every topology topologyStr does
// not name. Callers must pass the complete set of topologies a node belongs to.
func (c *client) SetNodeTopology(ctx context.Context, nodes, topologyStr string) error {
	if nodes == "" {
		return fmt.Errorf("set node topology: node expression is required")
	}

	response, err := c.SlurmV0044PostNodeWithResponse(ctx, nodes, api.V0044UpdateNodeMsg{
		TopologyStr: &topologyStr,
	})
	if err != nil {
		return fmt.Errorf("post topology for %s: %w", nodes, err)
	}
	if response.StatusCode() < http.StatusOK || response.StatusCode() >= http.StatusMultipleChoices {
		if topologyUnavailable(response.Body) {
			return fmt.Errorf("set topology %q for %s: %w", topologyStr, nodes, ErrTopologyUnavailable)
		}
		return fmt.Errorf(
			"set topology for %s failed: status=%d %s",
			nodes, response.StatusCode(), summarizeSlurmRESTBody(response.Body),
		)
	}
	if len(bytes.TrimSpace(response.Body)) == 0 {
		return nil
	}

	// A rejected update also comes back as 200 with the failure in the envelope, so the body is
	// checked even on success.
	var responseEnvelope api.V0044OpenapiResp
	if err := json.Unmarshal(response.Body, &responseEnvelope); err != nil {
		return fmt.Errorf("decode set topology response for %s: %w", nodes, err)
	}
	if responseEnvelope.Errors != nil && len(*responseEnvelope.Errors) > 0 {
		if topologyUnavailable(response.Body) {
			return fmt.Errorf("set topology %q for %s: %w", topologyStr, nodes, ErrTopologyUnavailable)
		}
		return fmt.Errorf(
			"set topology for %s responded with errors: %s",
			nodes, summarizeSlurmRESTBody(response.Body),
		)
	}

	return nil
}

func topologyUnavailable(body []byte) bool {
	var parsed struct {
		Errors []struct {
			Error string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	for _, e := range parsed.Errors {
		if e.Error == slurmErrTopologyUnavailable {
			return true
		}
	}
	return false
}
