package e2e

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsE2EResourceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: k8sClusterName, want: true},
		{name: k8sClusterName + "-jail", want: true},
		{name: k8sClusterName + "-fabric-5", want: true},
		{name: k8sClusterName + "ing", want: false},
		{name: "production-soperator", want: false},
		{name: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isE2EResourceName(tt.name))
		})
	}
}

func TestCleanupOrphanedCloudResourcesDeletesKnownKindsInDependencyOrder(t *testing.T) {
	t.Parallel()

	const projectID = "project-man-h100"
	script := []scriptedNebiusCommand{
		{
			args:   "mk8s cluster list --parent-id project-man-h100 --all --format json",
			output: resourceListJSON(resource("mk8s-1", k8sClusterName), resource("mk8s-other", k8sClusterName+"ing")),
		},
		{args: "mk8s cluster delete --id mk8s-1 --async --no-progress"},
		{args: "mk8s cluster list --parent-id project-man-h100 --all --format json", output: resourceListJSON()},
		{
			args:   "compute gpu-cluster list --parent-id project-man-h100 --all --format json",
			output: resourceListJSON(resource("gpu-1", k8sClusterName+"-fabric-4")),
		},
		{args: "compute gpu-cluster delete --id gpu-1 --async --no-progress"},
		{args: "compute gpu-cluster list --parent-id project-man-h100 --all --format json", output: resourceListJSON()},
		{
			args: "compute filesystem list --parent-id project-man-h100 --all --format json",
			output: resourceListJSON(
				resource("fs-jail", k8sClusterName+"-jail"),
				resource("fs-spool", k8sClusterName+"-controller-spool"),
			),
		},
		{args: "compute filesystem delete --id fs-spool --async --no-progress"},
		{args: "compute filesystem delete --id fs-jail --async --no-progress"},
		{args: "compute filesystem list --parent-id project-man-h100 --all --format json", output: resourceListJSON()},
		{
			args:   "vpc allocation list --parent-id project-man-h100 --all --format json",
			output: resourceListJSON(resource("allocation-1", k8sClusterName+"-public-static-ip")),
		},
		{args: "vpc allocation delete --id allocation-1 --async --no-progress"},
		{args: "vpc allocation list --parent-id project-man-h100 --all --format json", output: resourceListJSON()},
	}
	runner := newScriptedNebiusRunner(t, script)

	err := cleanupOrphanedCloudResourcesWith(
		context.Background(),
		projectID,
		orphanResourceKinds,
		runner,
		func(context.Context) error { return nil },
	)

	require.NoError(t, err)
}

func TestCleanupOrphanedCloudResourcesRejectsEmptyProjectID(t *testing.T) {
	t.Parallel()

	err := cleanupOrphanedCloudResourcesWith(
		context.Background(),
		"",
		orphanResourceKinds,
		func(context.Context, ...string) ([]byte, error) {
			t.Fatal("runner must not be called")
			return nil, nil
		},
		func(context.Context) error {
			t.Fatal("waiter must not be called")
			return nil
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "project ID")
}

func TestCleanupOrphanedCloudResourcesFailsOnListError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("permission denied")
	err := cleanupOrphanedCloudResourcesWith(
		context.Background(),
		"project-man-h200",
		orphanResourceKinds[:1],
		func(context.Context, ...string) ([]byte, error) {
			return nil, sentinel
		},
		func(context.Context) error {
			t.Fatal("waiter must not be called")
			return nil
		},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func TestCleanupOrphanedCloudResourcesRetriesDeleteFailure(t *testing.T) {
	t.Parallel()

	script := []scriptedNebiusCommand{
		{
			args:   "mk8s cluster list --parent-id project-man-h200 --all --format json",
			output: resourceListJSON(resource("mk8s-1", k8sClusterName)),
		},
		{
			args:   "mk8s cluster delete --id mk8s-1 --async --no-progress",
			output: "still provisioning",
			err:    errors.New("resource cannot be deleted while provisioning"),
		},
		{
			args:   "mk8s cluster list --parent-id project-man-h200 --all --format json",
			output: resourceListJSON(resource("mk8s-1", k8sClusterName)),
		},
		{args: "mk8s cluster delete --id mk8s-1 --async --no-progress"},
		{args: "mk8s cluster list --parent-id project-man-h200 --all --format json", output: resourceListJSON()},
	}
	runner := newScriptedNebiusRunner(t, script)

	err := cleanupOrphanedCloudResourcesWith(
		context.Background(),
		"project-man-h200",
		orphanResourceKinds[:1],
		runner,
		func(context.Context) error { return nil },
	)

	require.NoError(t, err)
}

func TestCleanupOrphanedCloudResourcesFailsWhenResourceRemains(t *testing.T) {
	t.Parallel()

	sentinel := context.DeadlineExceeded
	calls := 0
	err := cleanupOrphanedCloudResourcesWith(
		context.Background(),
		"project-man-h200",
		orphanResourceKinds[:1],
		func(_ context.Context, args ...string) ([]byte, error) {
			calls++
			if strings.Contains(strings.Join(args, " "), " list ") {
				return []byte(resourceListJSON(resource("mk8s-stuck", k8sClusterName))), nil
			}
			return []byte("still provisioning"), errors.New("resource cannot be deleted while provisioning")
		},
		func(context.Context) error { return sentinel },
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "soperator-e2e-test=mk8s-stuck")
	assert.Equal(t, 2, calls)
}

type scriptedNebiusCommand struct {
	args   string
	output string
	err    error
}

func newScriptedNebiusRunner(t *testing.T, script []scriptedNebiusCommand) nebiusCommandRunner {
	t.Helper()

	next := 0
	t.Cleanup(func() {
		assert.Equal(t, len(script), next, "not all scripted Nebius commands were called")
	})

	return func(_ context.Context, args ...string) ([]byte, error) {
		t.Helper()
		if next >= len(script) {
			t.Fatalf("unexpected Nebius command: %s", strings.Join(args, " "))
		}
		command := script[next]
		next++
		assert.Equal(t, command.args, strings.Join(args, " "))
		return []byte(command.output), command.err
	}
}

func resource(id, name string) string {
	return fmt.Sprintf(`{"metadata":{"id":%q,"name":%q}}`, id, name)
}

func resourceListJSON(resources ...string) string {
	return fmt.Sprintf(`{"items":[%s]}`, strings.Join(resources, ","))
}
