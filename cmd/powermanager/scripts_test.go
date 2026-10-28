package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func TestPowerActionScript(t *testing.T) {
	source, err := os.ReadFile("../../images/controller/power_action.sh")
	require.NoError(t, err)
	for _, tt := range []struct {
		name, action, timeout, want string
		exit                        int
		wait                        bool
	}{
		{name: "resume budget", action: "resume", timeout: "1800", want: "1795s", wait: true},
		{name: "suspend budget", action: "suspend", timeout: "90", want: "85s", wait: true},
		{name: "power failure propagates", action: "resume", timeout: "1800", want: "1795s", wait: true, exit: 7},
		{name: "invalid resume timeout", action: "resume", timeout: "INFINITE"},
		{name: "invalid suspend timeout", action: "suspend", timeout: "INFINITE"},
		{name: "missing timeout", action: "suspend"},
		{name: "zero timeout", action: "suspend", timeout: "0"},
		{name: "overflow timeout", action: "suspend", timeout: "18446744073709551616"},
		{name: "insufficient readiness budget", action: "suspend", timeout: "5", want: "5s"},
		{name: "minimum budget still writes", action: "suspend", timeout: "1", want: "1s"},
		{name: "minimum readiness budget", action: "resume", timeout: "6", want: "1s", wait: true},
		{name: "fallback power failure propagates", action: "suspend", exit: 7},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			argsFile := filepath.Join(dir, "args")
			mockPower := filepath.Join(dir, "power-manager")
			require.NoError(t, os.WriteFile(mockPower, []byte("#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$MOCK_ARGS_FILE\"\nexit \"$MOCK_EXIT\"\n"), 0700))
			rpcFile := filepath.Join(dir, "rpc")
			require.NoError(t, os.WriteFile(filepath.Join(dir, "scontrol"), []byte("#!/bin/bash\ntouch \"$MOCK_RPC_FILE\"\nexit 1\n"), 0700))
			script := filepath.Join(dir, "power_action.sh")
			require.NoError(t, os.WriteFile(script, []byte(strings.ReplaceAll(string(source), "/opt/soperator/bin/power-manager", mockPower)), 0600))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", script, tt.action, "worker-[0-3]")
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "POWER_MANAGER_TIMEOUT="+tt.timeout,
				"MOCK_EXIT="+strconv.Itoa(tt.exit), "MOCK_RPC_FILE="+rpcFile,
				"MOCK_ARGS_FILE="+argsFile, "POWER_MANAGER_REST_CONFIG_QPS=2", "POWER_MANAGER_REST_CONFIG_BURST=3")
			out, err := cmd.CombinedOutput()
			require.NoFileExists(t, rpcFile, "power actions must not query slurmctld before applying desired state")
			if tt.exit != 0 {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr, string(out))
				require.Equal(t, tt.exit, exitErr.ExitCode())
			} else {
				require.NoError(t, err, string(out))
			}
			argsBytes, err := os.ReadFile(argsFile)
			require.NoError(t, err, "power-manager must run even without the generated timeout")
			args := strings.Fields(string(argsBytes))
			require.GreaterOrEqual(t, len(args), 7)
			require.Equal(t, []string{tt.action, "--nodes", "worker-[0-3]", "--rest-config-qps", "2", "--rest-config-burst", "3"}, args[:7])
			if tt.want != "" {
				flags := args[7:]
				if tt.wait {
					require.Len(t, flags, 3)
					require.Equal(t, "--wait", flags[0])
					flags = flags[1:]
				}
				require.Len(t, flags, 2)
				require.Equal(t, "--timeout", flags[0])
				require.Equal(t, tt.want, flags[1])
			} else {
				require.Len(t, args, 7, "fallback must update desired state without waiting for readiness")
			}
		})
	}
}

func TestRenderedPowerActions(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "power-manager"), []byte("#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$MOCK_ARGS_FILE\"\n"), 0700))
	for _, script := range []string{"power_action.sh", "power_resume.sh", "power_suspend.sh", "power_resume_fail.sh"} {
		source, err := os.ReadFile(filepath.Join("../../images/controller", script))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, script), []byte(strings.ReplaceAll(string(source), "/opt/soperator/bin/", dir+"/")), 0700))
	}

	cluster := &values.SlurmCluster{}
	for _, config := range []slurmv1.SlurmConfig{
		{ResumeTimeout: ptr.To[int32](1800), SuspendTimeout: ptr.To[int32](90)},
		{ResumeTimeout: ptr.To[int32](3600), SuspendTimeout: ptr.To[int32](300)},
	} {
		cluster.SlurmConfig = config
		rendered := common.RenderConfigMapSlurmConfigs(cluster).Data[consts.ConfigMapKeySlurmBaseConfig]
		require.Contains(t, rendered, fmt.Sprintf("ResumeTimeout=%d", *config.ResumeTimeout))
		require.Contains(t, rendered, fmt.Sprintf("SuspendTimeout=%d", *config.SuspendTimeout))
		require.Contains(t, common.RenderJailedConfigSlurmConfigs(cluster).Spec.UpdateActions, slurmv1alpha1.UpdateActionReconfigure)
		programs := regexp.MustCompile(`(?m)^PowerAction=(soperator-(?:resume|resume-fail|suspend)) Location=slurmctld Program="([^"]+)"$`).FindAllStringSubmatch(rendered, -1)
		require.Len(t, programs, 3)
		for _, program := range programs {
			action, property, timeout := "suspend", "SuspendProgram", *config.SuspendTimeout
			switch program[1] {
			case "soperator-resume":
				action, property, timeout = "resume", "ResumeProgram", *config.ResumeTimeout
			case "soperator-resume-fail":
				property = "ResumeFailProgram"
			}
			require.Contains(t, rendered, property+"="+program[1])
			command := strings.Fields(strings.ReplaceAll(program[2], "/opt/soperator/bin/", dir+"/"))
			// Slurm appends the hostlist after the configured PowerAction arguments.
			command = append(command, "worker-[0-3],gpu-5")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cmd := exec.CommandContext(ctx, command[0], command[1:]...)
			cmd.Env = append(os.Environ(), "MOCK_ARGS_FILE="+argsFile, "POWER_MANAGER_REST_CONFIG_QPS=2", "POWER_MANAGER_REST_CONFIG_BURST=3")
			out, err := cmd.CombinedOutput()
			cancel()
			require.NoError(t, err, string(out))
			args, err := os.ReadFile(argsFile)
			require.NoError(t, err)
			require.Equal(t, []string{action, "--nodes", "worker-[0-3],gpu-5", "--rest-config-qps", "2", "--rest-config-burst", "3", "--wait", "--timeout", fmt.Sprintf("%ds", timeout-5)}, strings.Fields(string(args)))
		}
	}
}
