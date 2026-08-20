package v1alpha1_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionstest "k8s.io/apiextensions-apiserver/pkg/test"
)

func TestNodeSetMaxUnavailableCELValidation(t *testing.T) {
	validator, err := apiextensionstest.VersionValidatorFromFile(
		t,
		filepath.Join("..", "..", "config", "crd", "bases", "slurm.nebius.ai_nodesets.yaml"),
		"v1alpha1",
	)
	require.NoError(t, err)

	tests := []struct {
		name           string
		replicas       int64
		maxUnavailable any
		wantValid      bool
	}{
		{name: "absolute value at limit", replicas: 10_000, maxUnavailable: int64(500), wantValid: true},
		{name: "absolute value above limit", replicas: 10_000, maxUnavailable: int64(501), wantValid: false},
		{name: "percentage resolving to limit", replicas: 10_000, maxUnavailable: "5%", wantValid: true},
		{name: "percentage resolving above limit", replicas: 10_000, maxUnavailable: "10%", wantValid: false},
		{name: "percentage rounded down to limit", replicas: 10_019, maxUnavailable: "5%", wantValid: true},
		{name: "percentage rounded down above limit", replicas: 10_020, maxUnavailable: "5%", wantValid: false},
		{name: "zero value", replicas: 10_000, maxUnavailable: int64(0), wantValid: false},
		{name: "invalid percentage", replicas: 10_000, maxUnavailable: "5.5%", wantValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validator(map[string]any{
				"spec": map[string]any{
					"replicas":       tt.replicas,
					"maxUnavailable": tt.maxUnavailable,
				},
			}, nil)

			if tt.wantValid {
				assert.Empty(t, errs)
				return
			}

			if assert.NotEmpty(t, errs) {
				assert.Contains(t, errs.ToAggregate().Error(), "maxUnavailable must resolve to no more than 500 workers")
			}
		})
	}
}
