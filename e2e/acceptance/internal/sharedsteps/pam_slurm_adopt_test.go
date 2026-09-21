package sharedsteps

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPAMSlurmAdoptEnabled(t *testing.T) {
	for name, test := range map[string]struct {
		config  json.RawMessage
		enabled bool
	}{
		"omitted": {},
		"null": {
			config: json.RawMessage(`null`),
		},
		"empty object": {
			config: json.RawMessage(`{}`),
		},
		"disabled": {
			config: json.RawMessage(`{"enabled":false}`),
		},
		"enabled": {
			config:  json.RawMessage(`{"enabled":true}`),
			enabled: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			enabled, err := pamSlurmAdoptEnabled(test.config)
			require.NoError(t, err)
			assert.Equal(t, test.enabled, enabled)
		})
	}
}

func TestParsePAMSlurmAdoptStatus(t *testing.T) {
	status, err := parsePAMSlurmAdoptStatus("cgroup=0::/system.slice/slurmstepd.scope/job_42/step_extern/user\ngpu_count=1\n")
	require.NoError(t, err)
	assert.Equal(t, pamSlurmAdoptStatus{
		cgroup:   "0::/system.slice/slurmstepd.scope/job_42/step_extern/user",
		gpuCount: 1,
	}, status)
}

func TestParsePAMSlurmAdoptStatusRejectsInvalidOutput(t *testing.T) {
	for name, output := range map[string]string{
		"missing cgroup":    "gpu_count=1",
		"missing gpu count": "cgroup=0::/job_1/step_extern",
		"invalid gpu count": "cgroup=0::/job_1/step_extern\ngpu_count=all",
		"invalid line":      "cgroup=0::/job_1/step_extern\ngpu_count=1\ninvalid",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parsePAMSlurmAdoptStatus(output)
			assert.Error(t, err)
		})
	}
}

func TestIsSlurmExternCgroup(t *testing.T) {
	for name, test := range map[string]struct {
		value string
		want  bool
	}{
		"classic path": {
			value: "0::/slurm/uid_1000/job_42/step_extern",
			want:  true,
		},
		"scope path": {
			value: "0::/system.slice/slurmstepd.scope/123456789/step_extern/user/task",
			want:  true,
		},
		"cgroup v1": {
			value: "11:memory:/slurm/uid_1000/job_42/step_extern",
			want:  true,
		},
		"batch step": {
			value: "0::/slurm/uid_1000/job_42/step_batch",
		},
		"substring only": {
			value: "0::/slurm/uid_1000/job_42/step_extern_child",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, isSlurmExternCgroup(test.value))
		})
	}
}

func TestSlurmSettingContains(t *testing.T) {
	assert.True(t, slurmSettingContains("use_interactive_step,ULIMIT_PAM_ADOPT", "ulimit_pam_adopt"))
	assert.False(t, slurmSettingContains("use_interactive_step", "ulimit_pam_adopt"))
}
