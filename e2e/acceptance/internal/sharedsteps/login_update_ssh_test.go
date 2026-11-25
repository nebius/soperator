package sharedsteps

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestLoginUpdateSSHPort(t *testing.T) {
	for _, tc := range []struct {
		name       string
		containers []corev1.Container
		want       int32
	}{
		{name: "named custom port", containers: []corev1.Container{{Name: "sshd", Ports: []corev1.ContainerPort{
			{Name: "metrics", ContainerPort: 8080}, {Name: "sshd", ContainerPort: 2222},
		}}}, want: 2222},
		{name: "single unnamed TCP port", containers: []corev1.Container{{Name: "sshd", Ports: []corev1.ContainerPort{
			{ContainerPort: 2022}, {Protocol: corev1.ProtocolUDP, ContainerPort: 53},
		}}}, want: 2022},
		{name: "other container ignored", containers: []corev1.Container{{Name: "sidecar", Ports: []corev1.ContainerPort{{ContainerPort: 22}}}}},
		{name: "ambiguous ports", containers: []corev1.Container{{Name: "sshd", Ports: []corev1.ContainerPort{
			{ContainerPort: 22}, {ContainerPort: 2222},
		}}}},
		{name: "invalid named port", containers: []corev1.Container{{Name: "sshd", Ports: []corev1.ContainerPort{{Name: "sshd", ContainerPort: 70000}}}}},
		{name: "no declared port", containers: []corev1.Container{{Name: "sshd"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port, err := loginUpdateSSHPort(corev1.Pod{Spec: corev1.PodSpec{Containers: tc.containers}})
			if tc.want == 0 {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, port)
		})
	}
}

func TestLoginUpdateSSHCommandUsesTTYAndCapturesClientStderr(t *testing.T) {
	for _, probe := range []loginUpdateSSHProbe{
		{name: "login", shell: "bash -il", pty: true},
		{name: "nonlogin", shell: "bash -i", pty: true},
		{name: "command"},
		{name: "command-pty", pty: true},
	} {
		t.Run(probe.name, func(t *testing.T) {
			directory := t.TempDir()
			argsPath := filepath.Join(directory, "args")
			inputPath := filepath.Join(directory, "input")
			t.Setenv("SOPERATOR_TEST_SSH_ARGS", argsPath)
			t.Setenv("SOPERATOR_TEST_SSH_INPUT", inputPath)
			t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "timeout"), []byte("#!/bin/sh\nshift\nexec \"$@\"\n"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "ssh"), []byte(`#!/bin/sh
printf '%s\n' "$@" > "$SOPERATOR_TEST_SSH_ARGS"
cat > "$SOPERATOR_TEST_SSH_INPUT"
printf 'result\n'
printf 'client stderr\n' >&2
`), 0o755))
			command := loginUpdateSSHCommand(2222, probe)
			output, err := exec.CommandContext(t.Context(), "bash", "-c", command).Output()
			require.NoError(t, err)
			assert.Equal(t, "result\nclient stderr\n", string(output))
			arguments, err := os.ReadFile(argsPath)
			require.NoError(t, err)
			args := strings.Split(strings.TrimSuffix(string(arguments), "\n"), "\n")
			ttyFlag := "-T"
			if probe.pty {
				ttyFlag = "-tt"
			}
			assert.Equal(t, ttyFlag, args[0])
			assert.Contains(t, args, "BatchMode=yes")
			assert.Contains(t, args, "ConnectTimeout=10")
			require.GreaterOrEqual(t, len(args), 4)
			assert.Equal(t, []string{"-p", "2222", "127.0.0.1"}, args[len(args)-4:len(args)-1])
			input, err := os.ReadFile(inputPath)
			require.NoError(t, err)
			if probe.shell != "" {
				assert.Equal(t, probe.shell, args[len(args)-1])
				assert.Equal(t, loginUpdateSSHResultCommand+"; exit\n", string(input))
				assert.NotContains(t, string(input), loginUpdateSSHMarker)
			} else {
				assert.Equal(t, loginUpdateSSHResultCommand, args[len(args)-1])
				assert.Empty(t, input)
			}
			assert.NotContains(t, command, "login_warning")
		})
	}
}

func TestValidateLoginUpdateSSHTranscript(t *testing.T) {
	for _, tc := range []struct {
		name        string
		output      string
		interactive bool
		wantErr     string
	}{
		{name: "interactive warning", output: loginUpdateWarningText + "\r\nroot# " + loginUpdateSSHMarker + "\r\n", interactive: true},
		{name: "missing interactive warning", output: loginUpdateSSHMarker, interactive: true, wantErr: "outdated login warning"},
		{name: "echoed command is not completion", output: loginUpdateWarningText + "\n" + loginUpdateSSHResultCommand, interactive: true, wantErr: "completion marker"},
		{name: "plain command", output: loginUpdateSSHMarker + "\n"},
		{name: "PTY command", output: loginUpdateSSHMarker + "\r\n"},
		{name: "custom SSH banner", output: "Customer SSH banner\n" + loginUpdateSSHMarker},
		{name: "warning leaks to command", output: loginUpdateWarningText + "\n" + loginUpdateSSHMarker, wantErr: "outdated login warning appeared"},
		{name: "command never ran", output: "", wantErr: "completion marker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLoginUpdateSSHTranscript(tc.output, tc.interactive)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

type loginUpdateSSHRuntime struct {
	framework.Runtime
	pod              corev1.Pod
	replaceAfterExec bool
	execArgs         []string
}

func (r *loginUpdateSSHRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		if args[0] == "get" {
			data, err := json.Marshal(r.pod)
			return string(data), err
		}
		r.execArgs = append([]string(nil), args...)
		if r.replaceAfterExec {
			r.pod.UID = "replacement"
		}
		return "transcript", nil
	})
}

func TestLoginUpdateSSHProbePinsTheSelectedPod(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "replaced"}[replacement], func(t *testing.T) {
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "login-2", UID: "original"}}
			runtime := &loginUpdateSSHRuntime{pod: pod, replaceAfterExec: replacement}
			steps := &LoginUpdates{loginWorkload: loginWorkload{runtime: runtime, kubectl: framework.NewKubectlClient(runtime)}, probePod: pod}
			output, err := steps.runLoginUpdateSSHProbe(t.Context(), "probe-command")
			assert.Equal(t, "transcript", output)
			assert.Equal(t, []string{"exec", "-n", framework.SoperatorNamespace, "login-2", "-c", "sshd", "--", "chroot", "/mnt/jail", "bash", "-lc", "probe-command"}, runtime.execArgs)
			if replacement {
				var permanent *framework.PermanentError
				require.ErrorAs(t, err, &permanent)
				require.ErrorContains(t, err, "uid=replacement expected=original")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
