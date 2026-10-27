package sharedsteps

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type PackageInstallation struct {
	runtime          framework.Runtime
	selector         *framework.WorkerSelector
	packageWorker    framework.WorkerInfo
	originalPackages map[string]bool
	installedPackage string
}

func NewPackageInstallation(runtime framework.Runtime, selector *framework.WorkerSelector) *PackageInstallation {
	return &PackageInstallation{runtime: runtime, selector: selector}
}

func (s *PackageInstallation) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the NVIDIA driver is working on a worker node$`, s.theNVIDIADriverIsWorkingOnAWorkerNode)
	sc.Step(`^the NVIDIA driver is still working on the worker node$`, s.theNVIDIADriverIsStillWorkingOnTheWorkerNode)
	sc.Step(`^package "([^"]+)" is not installed on the worker node$`, s.packageIsNotInstalled)
	sc.Step(`^package "([^"]+)" is installed on the worker node$`, s.installPackage)
	sc.Step(`^package "([^"]+)" is uninstalled from the worker node$`, s.uninstallPackage)
	sc.Step(`^command "([^"]+)" succeeds on the worker node$`, s.commandSucceeds)
}

func (s *PackageInstallation) CleanupAndReset(ctx context.Context) {
	if s.installedPackage != "" {
		cleanupCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		if err := s.uninstallPackage(cleanupCtx, s.installedPackage); err != nil {
			s.runtime.Logf("Cleanup package installation on %s: %v", s.packageWorker.Name, err)
		}
	}
	s.packageWorker = framework.WorkerInfo{}
	s.originalPackages = nil
	s.installedPackage = ""
}

func (s *PackageInstallation) theNVIDIADriverIsWorkingOnAWorkerNode(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.packageWorker = workers[0]

	if _, err := s.runtime.Worker(s.packageWorker).RunWithDefaultRetry(ctx, "nvidia-smi >/dev/null"); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker)
		return fmt.Errorf("verify nvidia-smi before install: %w", err)
	}
	return nil
}

func (s *PackageInstallation) installDebianPackage(ctx context.Context, name string) error {
	if _, err := s.runtime.Worker(s.packageWorker).Run(ctx, "DEBIAN_FRONTEND=noninteractive apt-get update"); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker, name)
		return fmt.Errorf("update APT indexes on %s: %w", s.packageWorker.Name, err)
	}
	command := "DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends " + framework.ShellQuote(name)
	if _, err := s.runtime.Worker(s.packageWorker).Run(ctx, command); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker, name)
		return fmt.Errorf("install package %s on %s: %w", name, s.packageWorker.Name, err)
	}
	return nil
}

func (s *PackageInstallation) theNVIDIADriverIsStillWorkingOnTheWorkerNode(ctx context.Context) error {
	if _, err := s.runtime.Worker(s.packageWorker).RunWithDefaultRetry(ctx, "nvidia-smi >/dev/null"); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker)
		return fmt.Errorf("verify nvidia-smi on worker %s: %w", s.packageWorker.Name, err)
	}
	return nil
}

func (s *PackageInstallation) logInstallFailureDiagnostics(ctx context.Context, worker framework.WorkerInfo, packages ...string) {
	var commands = []string{
		"dpkg --audit || true",
		"tail -n 60 /var/log/dpkg.log || true",
		"tail -n 60 /var/log/apt/term.log || true",
	}
	if len(packages) > 0 {
		var quoted []string
		for _, name := range packages {
			quoted = append(quoted, framework.ShellQuote(name))
		}
		commands = append(commands, "apt-cache policy "+strings.Join(quoted, " ")+" || true")
	}

	for _, command := range commands {
		output, err := s.runtime.Worker(worker).RunWithRetry(ctx, command, 2, 10*time.Second)
		if err != nil {
			s.runtime.Logf("Package installation debug command failed on %s (%s): %v", worker.Name, command, err)
			continue
		}
		s.runtime.Logf("Package installation debug output on %s (%s):\n%s", worker.Name, command, strings.TrimSpace(output))
	}
}

var debianPackageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+(:[a-z0-9][a-z0-9-]*)?$`)

const packageInventoryCommand = "dpkg-query -W -f='${binary:Package}\\t${db:Status-Status}\\n'"

func parsePackageInventory(output string) (map[string]bool, error) {
	packages := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		name, status, ok := strings.Cut(line, "\t")
		if !ok || !debianPackageName.MatchString(name) {
			return nil, fmt.Errorf("parse package inventory row %q", line)
		}
		switch status {
		case "installed", "unpacked", "half-installed", "half-configured", "triggers-awaited", "triggers-pending":
			packages[name] = true
		case "not-installed", "config-files":
		default:
			return nil, fmt.Errorf("parse package %s status %q", name, status)
		}
	}
	return packages, nil
}

func (s *PackageInstallation) packageInventory(ctx context.Context) (map[string]bool, error) {
	output, err := s.runtime.Worker(s.packageWorker).Run(ctx, packageInventoryCommand)
	if err != nil {
		return nil, fmt.Errorf("read package inventory on %s: %w; output: %s", s.packageWorker.Name, err, output)
	}
	return parsePackageInventory(output)
}

func (s *PackageInstallation) packageIsNotInstalled(ctx context.Context, name string) error {
	if !debianPackageName.MatchString(name) {
		return fmt.Errorf("validate Debian package name %q", name)
	}
	packages, err := s.packageInventory(ctx)
	if err != nil {
		return err
	}
	for installed := range packages {
		if installed == name || strings.HasPrefix(installed, name+":") {
			return fmt.Errorf("expected package %s absent on worker %s, found %s", name, s.packageWorker.Name, installed)
		}
	}
	return nil
}

func (s *PackageInstallation) installPackage(ctx context.Context, name string) error {
	if s.installedPackage != "" {
		return fmt.Errorf("install %s: package %s already tracked for cleanup", name, s.installedPackage)
	}
	if !debianPackageName.MatchString(name) {
		return fmt.Errorf("validate Debian package name %q", name)
	}
	packages, err := s.packageInventory(ctx)
	if err != nil {
		return err
	}
	for installed := range packages {
		if installed == name || strings.HasPrefix(installed, name+":") {
			return s.installDebianPackage(ctx, name)
		}
	}
	// Capture the baseline before APT runs, including when installation only partially succeeds.
	s.originalPackages = packages
	s.installedPackage = name
	return s.installDebianPackage(ctx, name)
}

func (s *PackageInstallation) uninstallPackage(ctx context.Context, name string) error {
	if name != s.installedPackage || s.originalPackages == nil {
		return fmt.Errorf("uninstall %s: no matching package installation baseline", name)
	}
	packages, err := s.packageInventory(ctx)
	if err != nil {
		return err
	}
	var added []string
	for installed := range packages {
		if !s.originalPackages[installed] {
			added = append(added, installed)
		}
	}
	sort.Strings(added)
	if len(added) > 0 {
		var quoted []string
		for _, installed := range added {
			quoted = append(quoted, framework.ShellQuote(installed))
		}
		// Remove only packages introduced by this installation, avoiding global autoremove.
		command := "DEBIAN_FRONTEND=noninteractive apt-get remove -y --no-auto-remove " + strings.Join(quoted, " ")
		if _, err := s.runtime.Worker(s.packageWorker).Run(ctx, command); err != nil {
			s.logInstallFailureDiagnostics(ctx, s.packageWorker, added...)
			return fmt.Errorf("remove packages %v on %s: %w", added, s.packageWorker.Name, err)
		}
	}
	if err := s.packageIsNotInstalled(ctx, name); err != nil {
		return err
	}
	s.installedPackage = ""
	s.originalPackages = nil
	return nil
}

func (s *PackageInstallation) commandSucceeds(ctx context.Context, command string) error {
	output, err := s.runtime.Worker(s.packageWorker).Run(ctx, "timeout 30s "+framework.BashLC(command))
	if err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker)
		return fmt.Errorf("run %q on worker %s: %w; output: %s", command, s.packageWorker.Name, err, output)
	}
	s.runtime.Logf("Command %q on worker %s:\n%s", command, s.packageWorker.Name, strings.TrimSpace(output))
	return nil
}
