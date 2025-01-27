package checkcontroller

import (
	"encoding/json"
	"fmt"
	"strings"
)

// unmarshalSlurmJSON unmarshals the JSON output from scontrol show nodes --json
func unmarshalSlurmJSON(output []byte) (map[string]interface{}, error) {
	var slurmData map[string]interface{}
	err := json.Unmarshal(output, &slurmData)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON: %w", err)
	}
	return slurmData, nil
}

// findNodesByStateAndReason finds nodes in the DRAIN state with the reason "Kill task failed"
func findNodesByStateAndReason(slurmData map[string]interface{}) ([]string, error) {
	var matchingNodes []string
	nodes, ok := slurmData["nodes"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("error extracting nodes from JSON")
	}

	for _, node := range nodes {
		nodeData, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		state, stateOk := nodeData["state"].([]interface{})
		reason, reasonOk := nodeData["reason"].(string)
		if stateOk && reasonOk {
			for _, s := range state {
				if s == "DRAIN" && strings.Contains(reason, "Kill task failed") {
					name, _ := nodeData["name"].(string)
					matchingNodes = append(matchingNodes, name)
				}
			}
		}
	}

	return matchingNodes, nil
}
