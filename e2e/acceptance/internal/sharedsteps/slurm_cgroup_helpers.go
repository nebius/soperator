package sharedsteps

import (
	"fmt"
	"path"
	"strings"
)

const (
	slurmStepdScope          = "slurmstepd.scope"
	slurmSLUIDLength         = 14
	slurmSLUIDBase32Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

func slurmCgroupParentBelongsToJob(value, jobID string) bool {
	cleaned := path.Clean(strings.TrimSpace(value))
	if cleaned == "." || !strings.HasPrefix(cleaned, "/") || strings.TrimSpace(jobID) == "" {
		return false
	}

	components := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	if len(components) < 3 || components[len(components)-1] != "user" {
		return false
	}

	stepIndex := len(components) - 2
	jobIndex := stepIndex - 1
	if !isSlurmStepComponent(components[stepIndex]) {
		return false
	}
	if components[jobIndex] == "job_"+jobID {
		return true
	}

	// Slurm 26.05 uses an opaque SLUID instead of job_<id>. The caller binds the
	// process or container to jobID through its recorded identity, so validate
	// the strict SLUID hierarchy here.
	return jobIndex > 0 && components[jobIndex-1] == slurmStepdScope && isSlurmSLUID(components[jobIndex])
}

func isSlurmSLUID(value string) bool {
	if len(value) != slurmSLUIDLength || value[0] != 's' {
		return false
	}
	for _, character := range value[1:] {
		if !strings.ContainsRune(slurmSLUIDBase32Alphabet, character) {
			return false
		}
	}
	return true
}

func isSlurmStepComponent(value string) bool {
	if hasSlurmNumericSuffix(value, "step_") {
		return true
	}
	return value == "step_batch" || value == "step_extern" || value == "step_interactive"
}

func hasSlurmNumericSuffix(value, prefix string) bool {
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

func slurmJobCgroup(taskCgroup, jobID string) (string, error) {
	cleaned := path.Clean(taskCgroup)
	if cleaned != taskCgroup || !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("validate Slurm task cgroup %q", taskCgroup)
	}
	for current := cleaned; current != "/"; current = path.Dir(current) {
		if isSlurmStepComponent(path.Base(current)) && slurmCgroupParentBelongsToJob(current+"/user", jobID) {
			return path.Join("/sys/fs/cgroup", path.Dir(current)), nil
		}
	}
	return "", fmt.Errorf("find job %s cgroup in %q", jobID, taskCgroup)
}
