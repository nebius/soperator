package slurmvalues

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNewDefaultsMatchValuesYAML(t *testing.T) {
	b, err := os.ReadFile("../../../../helm/slurm-cluster/values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var orig map[string]interface{}
	if err := yaml.Unmarshal(b, &orig); err != nil {
		t.Fatal(err)
	}
	out, err := yaml.Marshal(NewDefaults())
	if err != nil {
		t.Fatal(err)
	}
	var gen map[string]interface{}
	if err := yaml.Unmarshal(out, &gen); err != nil {
		t.Fatal(err)
	}
	if ok, reason := subsetEqualReason(orig, gen, ""); !ok {
		t.Fatalf("generated defaults don't match values.yaml: %s", reason)
	}
}

func subsetEqualReason(a, b interface{}, path string) (bool, string) {
	switch aa := a.(type) {
	case map[string]interface{}:
		bb, ok := b.(map[string]interface{})
		if !ok {
			return false, path + " (expected map)"
		}
		for k, v := range aa {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if ok, reason := subsetEqualReason(v, bb[k], p); !ok {
				return false, reason
			}
		}
		return true, ""
	case []interface{}:
		bb, ok := b.([]interface{})
		if !ok {
			return false, path + " (expected slice)"
		}
		if len(aa) != len(bb) {
			return false, path + " (slice length mismatch)"
		}
		for i := range aa {
			p := fmt.Sprintf("%s[%d]", path, i)
			if ok, reason := subsetEqualReason(aa[i], bb[i], p); !ok {
				return false, reason
			}
		}
		return true, ""
	case float64:
		switch bb := b.(type) {
		case float64:
			if aa == bb {
				return true, ""
			}
			return false, path + " (number mismatch)"
		case int:
			if aa == float64(bb) {
				return true, ""
			}
			return false, path + " (number mismatch)"
		default:
			return false, path + " (type mismatch)"
		}
	default:
		if reflect.DeepEqual(a, b) {
			return true, ""
		}
		return false, path + " (value mismatch)"
	}
}
