package sharedsteps

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	loginUpdateWarningText       = "Warning: This login node is outdated."
	loginUpdateSSHMarker         = "__SOPERATOR_LOGIN_UPDATE_SSH_OK__"
	loginUpdateSSHResultCommand  = "printf '%s%s\\n' '__SOPERATOR_LOGIN_UPDATE_' 'SSH_OK__'"
	loginUpdateProjectionTimeout = 2 * time.Minute
	loginUpdateSSHTimeout        = 45 * time.Second
)

type loginUpdateSSHProbe struct {
	name  string
	shell string
	pty   bool
}

func (s *LoginUpdates) checkLoginUpdateWarnings(ctx context.Context) error {
	port, err := loginUpdateSSHPort(s.probePod)
	if err != nil {
		return err
	}
	announced := s.probePod.Annotations[loginUpdateAnnounced]
	scheduled := s.probePod.Annotations[loginUpdateScheduled]
	if announced == "" || scheduled == "" {
		return fmt.Errorf("read warning schedule for login pod %s: announcement=%q schedule=%q", s.probePod.Name, announced, scheduled)
	}

	projection := "announced=$(cat /run/soperator-login-state/announced-at) && " +
		"scheduled=$(cat /run/soperator-login-state/scheduled-at) && " +
		`printf '%s\n%s\n' "$announced" "$scheduled"`
	var projectionOutput string
	err = s.runtime.WaitFor(ctx, "login warning schedule projection", loginUpdateProjectionTimeout, 2*time.Second,
		func(waitCtx context.Context) (bool, error) {
			var runErr error
			projectionOutput, runErr = s.runLoginUpdateSSHProbe(waitCtx, projection)
			if runErr != nil {
				return false, runErr
			}
			if projectionOutput != announced+"\n"+scheduled+"\n" {
				return false, fmt.Errorf("wait for warning projection in login pod %s: got %q, expected announcement=%q schedule=%q",
					s.probePod.Name, projectionOutput, announced, scheduled)
			}
			return true, nil
		})
	if err != nil {
		artifact := s.saveLoginUpdateSSHTranscript(ctx, "projection", projectionOutput)
		return fmt.Errorf("observe login warning metadata%s: %w", artifact, err)
	}

	probes := []loginUpdateSSHProbe{
		{name: "interactive-login", shell: "bash -il", pty: true},
		{name: "interactive-nonlogin", shell: "bash -i", pty: true},
		{name: "noninteractive"},
		{name: "noninteractive-pty", pty: true},
	}
	for _, probe := range probes {
		output, runErr := s.runLoginUpdateSSHProbe(ctx, loginUpdateSSHCommand(port, probe))
		artifact := s.saveLoginUpdateSSHTranscript(ctx, probe.name, output)
		if runErr != nil {
			return fmt.Errorf("run %s SSH probe on login pod %s%s: %w; transcript: %q",
				probe.name, s.probePod.Name, artifact, runErr, output)
		}
		if err := validateLoginUpdateSSHTranscript(output, probe.shell != ""); err != nil {
			return fmt.Errorf("check %s SSH probe on login pod %s%s: %w; transcript: %q",
				probe.name, s.probePod.Name, artifact, err, output)
		}
	}
	return nil
}

func loginUpdateSSHPort(pod corev1.Pod) (int32, error) {
	for _, container := range pod.Spec.Containers {
		if container.Name != loginSSHDContainer {
			continue
		}
		var ports []corev1.ContainerPort
		for _, port := range container.Ports {
			if port.Protocol != "" && port.Protocol != corev1.ProtocolTCP {
				continue
			}
			if port.Name == loginSSHDContainer {
				if port.ContainerPort < 1 || port.ContainerPort > 65535 {
					return 0, fmt.Errorf("read SSH port of login pod %s: invalid named port %d", pod.Name, port.ContainerPort)
				}
				return port.ContainerPort, nil
			}
			ports = append(ports, port)
		}
		if len(ports) == 1 && ports[0].ContainerPort > 0 && ports[0].ContainerPort <= 65535 {
			return ports[0].ContainerPort, nil
		}
		return 0, fmt.Errorf("read SSH port of login pod %s: expected one TCP port or a port named %s, got %v", pod.Name, loginSSHDContainer, ports)
	}
	return 0, fmt.Errorf("read SSH port of login pod %s: no %s container", pod.Name, loginSSHDContainer)
}

func loginUpdateSSHCommand(port int32, probe loginUpdateSSHProbe) string {
	ttyFlag := "-T"
	if probe.pty {
		ttyFlag = "-tt"
	}
	remoteCommand := loginUpdateSSHResultCommand
	if probe.shell != "" {
		remoteCommand = probe.shell
	}
	ssh := "TERM=dumb timeout 30 ssh " + ttyFlag +
		" -o BatchMode=yes -o ConnectTimeout=10 -o ConnectionAttempts=1 -o LogLevel=ERROR" +
		" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ControlPath=none" +
		" -p " + strconv.Itoa(int(port)) + " 127.0.0.1 " + framework.ShellQuote(remoteCommand) + " 2>&1"
	if probe.shell != "" {
		// Split the result marker so terminal input echo cannot satisfy the assertion.
		return "set -o pipefail; printf '%s\\n' " + framework.ShellQuote(loginUpdateSSHResultCommand+"; exit") + " | " + ssh
	}
	return ssh + " </dev/null"
}

func validateLoginUpdateSSHTranscript(output string, interactive bool) error {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	if !strings.Contains(output, loginUpdateSSHMarker) {
		return fmt.Errorf("find SSH command completion marker")
	}
	if interactive {
		if !strings.Contains(output, loginUpdateWarningText) {
			return fmt.Errorf("find outdated login warning in interactive shell")
		}
		return nil
	}
	if strings.Contains(output, loginUpdateWarningText) {
		return fmt.Errorf("outdated login warning appeared in a noninteractive session")
	}
	return nil
}

func (s *LoginUpdates) runLoginUpdateSSHProbe(ctx context.Context, command string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, loginUpdateSSHTimeout)
	defer cancel()
	if err := s.checkLoginUpdateProbeUID(commandCtx); err != nil {
		return "", err
	}
	output, err := s.runtime.Kubectl().Run(commandCtx, "exec", "-n", framework.SoperatorNamespace, s.probePod.Name,
		"-c", loginSSHDContainer, "--", "chroot", "/mnt/jail", "bash", "-lc", command)
	if uidErr := s.checkLoginUpdateProbeUID(commandCtx); uidErr != nil {
		return output, uidErr
	}
	return output, err
}

func (s *LoginUpdates) checkLoginUpdateProbeUID(ctx context.Context) error {
	current, present, err := s.kubectl.PodIfPresent(ctx, framework.SoperatorNamespace, s.probePod.Name)
	if err != nil {
		return err
	}
	if !present || current.UID != s.probePod.UID || current.DeletionTimestamp != nil {
		return &framework.PermanentError{Err: fmt.Errorf("keep login pod %s unchanged during SSH probes: present=%t uid=%s expected=%s deleting=%t",
			s.probePod.Name, present, current.UID, s.probePod.UID, current.DeletionTimestamp != nil)}
	}
	return nil
}

func (s *LoginUpdates) saveLoginUpdateSSHTranscript(ctx context.Context, name, output string) string {
	artifacts, ok := framework.ScenarioArtifacts(ctx)
	if !ok {
		return ""
	}
	filename := "login-update-ssh-" + name + ".log"
	if err := artifacts.WriteRunnerFile(filename, output); err != nil {
		s.runtime.Logf("Write login SSH probe transcript: %v", err)
		return ""
	}
	return " (transcript: " + filepath.Join(artifacts.RunnerDir, filename) + ")"
}
