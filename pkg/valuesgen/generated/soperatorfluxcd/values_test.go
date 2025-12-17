package fluxvalues

import (
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNewDefaultsMatchValuesYAML(t *testing.T) {
	b, err := os.ReadFile("../../../../helm/soperator-fluxcd/values.yaml")
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
	if !reflect.DeepEqual(orig, gen) {
		t.Fatalf("generated defaults don't match values.yaml")
	}
}
