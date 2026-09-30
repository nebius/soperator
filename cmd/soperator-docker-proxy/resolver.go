package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	modeLogin           = "login"
	modeWorker          = "worker"
	minimumLoginUserID  = 1000
	slurmStepdScope     = "slurmstepd.scope"
	sluidLength         = 14
	sluidBase32Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

type cgroupResolution struct {
	parent string
	source string
}

type cgroupResolver interface {
	Resolve(credentials peerCredentials) (cgroupResolution, error)
}

type loginCgroupResolver struct {
	base string
}

type workerCgroupResolver struct {
	procRoot string
}

func newCgroupResolver(ctx context.Context, cfg config) (cgroupResolver, error) {
	switch cfg.mode {
	case modeLogin:
		base, err := waitForCgroupBase(ctx, cfg.cgroupBaseFile, cfg.cgroupWaitTimeout)
		if err != nil {
			return nil, fmt.Errorf("resolve login cgroup base: %w", err)
		}
		return loginCgroupResolver{base: base}, nil
	case modeWorker:
		if cfg.procRoot == "" || !filepath.IsAbs(cfg.procRoot) {
			return nil, errors.New("worker proc root must be an absolute path")
		}
		return workerCgroupResolver{procRoot: filepath.Clean(cfg.procRoot)}, nil
	default:
		return nil, fmt.Errorf("mode must be %q or %q", modeLogin, modeWorker)
	}
}

func (resolver loginCgroupResolver) Resolve(credentials peerCredentials) (cgroupResolution, error) {
	if credentials.uid < minimumLoginUserID {
		return cgroupResolution{
			parent: resolver.base + "/docker-unattributed",
			source: "uid=0",
		}, nil
	}

	return cgroupResolution{
		parent: fmt.Sprintf("%s/users/user-%d/docker", resolver.base, credentials.uid),
		source: fmt.Sprintf("uid=%d", credentials.uid),
	}, nil
}

func (resolver workerCgroupResolver) Resolve(credentials peerCredentials) (cgroupResolution, error) {
	if credentials.pid <= 0 {
		return cgroupResolution{}, errors.New("Unix peer PID is unavailable")
	}

	cgroupFile := filepath.Join(resolver.procRoot, strconv.FormatInt(int64(credentials.pid), 10), "cgroup")
	contents, err := os.ReadFile(cgroupFile)
	if err != nil {
		return cgroupResolution{}, fmt.Errorf("read peer cgroup file %s: %w", cgroupFile, err)
	}
	peerPath, err := unifiedCgroupPath(string(contents))
	if err != nil {
		return cgroupResolution{}, fmt.Errorf("parse peer cgroup file %s: %w", cgroupFile, err)
	}

	parent, ok := slurmDockerCgroupParent(peerPath)
	if !ok {
		return cgroupResolution{source: peerPath}, nil
	}
	return cgroupResolution{parent: parent, source: peerPath}, nil
}

func unifiedCgroupPath(contents string) (string, error) {
	var result string
	for _, line := range strings.Split(contents, "\n") {
		if !strings.HasPrefix(line, "0::") {
			continue
		}
		if result != "" {
			return "", errors.New("cgroup file contains multiple unified hierarchy entries")
		}
		value := strings.TrimPrefix(line, "0::")
		cleaned, err := validateCgroupPath(value)
		if err != nil {
			return "", err
		}
		result = cleaned
	}
	if result == "" {
		return "", errors.New("cgroup file has no unified hierarchy entry")
	}
	return result, nil
}

func slurmDockerCgroupParent(cgroupPath string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(cgroupPath, "/"), "/")
	for userIndex := len(parts) - 1; userIndex >= 0; userIndex-- {
		if parts[userIndex] != "user" {
			continue
		}

		jobIndex := -1
		stepIndex := -1
		for index := 0; index < userIndex; index++ {
			if isSlurmJobComponent(parts, index) {
				jobIndex = index
			}
			if isSlurmStepComponent(parts[index]) {
				stepIndex = index
			}
		}
		if jobIndex >= 0 && stepIndex > jobIndex {
			return "/" + strings.Join(parts[:userIndex+1], "/"), true
		}
	}
	return "", false
}

func isSlurmJobComponent(parts []string, index int) bool {
	if hasNumericSuffix(parts[index], "job_") {
		return true
	}
	return index > 0 && parts[index-1] == slurmStepdScope && isSLUID(parts[index])
}

func isSLUID(value string) bool {
	if len(value) != sluidLength || value[0] != 's' {
		return false
	}
	for _, character := range value[1:] {
		if !strings.ContainsRune(sluidBase32Alphabet, character) {
			return false
		}
	}
	return true
}

func isSlurmStepComponent(component string) bool {
	if hasNumericSuffix(component, "step_") {
		return true
	}
	switch component {
	case "step_batch", "step_extern", "step_interactive":
		return true
	default:
		return false
	}
}

func hasNumericSuffix(value, prefix string) bool {
	suffix, ok := strings.CutPrefix(value, prefix)
	if !ok || suffix == "" {
		return false
	}
	for _, character := range suffix {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func waitForCgroupBase(ctx context.Context, filePath string, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		contents, err := os.ReadFile(filePath)
		if err == nil {
			base, validateErr := validateCgroupPath(string(contents))
			if validateErr == nil && base != "/" {
				return base, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read cgroup base file: %w", err)
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", fmt.Errorf("time out waiting for %s", filePath)
		case <-ticker.C:
		}
	}
}

func validateCgroupPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") {
		return "", errors.New("cgroup path must be absolute")
	}
	cleaned := path.Clean(value)
	if cleaned != value {
		return "", errors.New("cgroup path must be clean")
	}
	return cleaned, nil
}
