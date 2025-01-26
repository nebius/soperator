package checkcontroller

import (
	"encoding/json"
	"testing"
)

// TestFindNodesByStateAndReason tests the findNodesByStateAndReason function
func TestFindNodesByStateAndReason(t *testing.T) {
	// Example JSON data as if it was received from scontrol show nodes --json
	jsonData := []byte(`
		{
			"nodes": [
				{
					"name": "worker-0",
					"state": ["IDLE", "DRAIN", "DYNAMIC_NORM"],
					"reason": "Kill task failed : Not responding"
				},
				{
					"name": "worker-1",
					"state": ["IDLE", "DYNAMIC_NORM"],
					"reason": "Normal"
				},
				{
					"name": "worker-2",
					"state": ["IDLE", "DRAIN", "DYNAMIC_NORM"],
					"reason": "Kill task failed : Not responding"
				}
			]
		}
	`)

	expected := []string{"worker-0", "worker-2"}

	slurmData, err := unmarshalSlurmJSON(jsonData)
	if err != nil {
		t.Fatalf("Error unmarshaling JSON: %v", err)
	}

	result, err := findNodesByStateAndReason(slurmData)
	if err != nil {
		t.Fatalf("Error finding nodes: %v", err)
	}

	if len(result) != len(expected) {
		t.Errorf("Expected %v, but got %v", expected, result)
	}

	for i, name := range expected {
		if result[i] != name {
			t.Errorf("Expected node '%s', but got '%s'", name, result[i])
		}
	}
}

// TestUnmarshalSlurmJSON tests the unmarshalSlurmJSON function
func TestUnmarshalSlurmJSON(t *testing.T) {
	// Example JSON data as if it was received from scontrol show nodes --json
	jsonData := []byte(`
		{
			"nodes": [
				{
					"name": "worker-0",
					"state": ["IDLE", "DRAIN", "DYNAMIC_NORM"],
					"reason": "Kill task failed"
				},
				{
					"name": "worker-1",
					"state": ["IDLE", "DYNAMIC_NORM"],
					"reason": "Normal"
				}
			]
		}
	`)

	expected := map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{
				"name":   "worker-0",
				"state":  []interface{}{"IDLE", "DRAIN", "DYNAMIC_NORM"},
				"reason": "Kill task failed",
			},
			map[string]interface{}{
				"name":   "worker-1",
				"state":  []interface{}{"IDLE", "DYNAMIC_NORM"},
				"reason": "Normal",
			},
		},
	}

	result, err := unmarshalSlurmJSON(jsonData)
	if err != nil {
		t.Fatalf("Error unmarshaling JSON: %v", err)
	}

	if !compareJSON(result, expected) {
		t.Errorf("Expected %+v, but got %+v", expected, result)
	}
}

// Func compareJSON compares two JSON objects
func compareJSON(a, b interface{}) bool {
	aBytes, _ := json.Marshal(a)
	bBytes, _ := json.Marshal(b)
	return string(aBytes) == string(bBytes)
}
