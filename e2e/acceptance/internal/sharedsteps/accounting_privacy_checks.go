package sharedsteps

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

type accountingPrivacyJobRecord struct {
	ID         string
	User       string
	Account    string
	Name       string
	State      string
	AssocID    string
	WorkDir    string
	SubmitLine string
}

type accountingPrivacyIdentity struct {
	UID string
	GID string
}

func (p *accountingPrivacyState) permissions(actor, account string, coordinator, privileged bool) ([]accountingPrivacyJobRecord, []string, []string) {
	var jobs []accountingPrivacyJobRecord
	for _, job := range p.Jobs {
		if privileged || job.Record.User == actor || (coordinator && job.Record.Account == account) {
			jobs = append(jobs, job.Record)
		}
	}
	var users, associations []string
	for _, user := range p.Users {
		if user.Account == "" {
			continue
		}
		if privileged || user.Name == actor {
			users = append(users, user.Name)
		}
		if privileged || user.Name == actor || (coordinator && user.Account == account) {
			associations = append(associations, user.Name+"|"+user.Account)
		}
	}
	return jobs, users, associations
}

func captureAccountingPrivacyJobs(output string, submitted []accountingPrivacyJob) ([]accountingPrivacyJob, error) {
	records, err := parseAccountingPrivacyJobs(output)
	if err != nil {
		return nil, err
	}
	if len(records) != len(submitted) {
		return nil, fmt.Errorf("require %d complete privacy jobs, observed:\n%s", len(submitted), output)
	}
	jobs := slices.Clone(submitted)
	for i := range jobs {
		job := &jobs[i]
		index := slices.IndexFunc(records, func(record accountingPrivacyJobRecord) bool { return record.ID == job.Job.ID })
		if index < 0 {
			return nil, fmt.Errorf("find privacy job %s in %s", job.Job.ID, output)
		}
		record := records[index]
		assocID, err := strconv.ParseUint(record.AssocID, 10, 64)
		if err != nil || assocID == 0 || record.State != "COMPLETED" || record.User != job.Record.User ||
			record.Account != job.Record.Account || record.Name != job.Job.JobName || record.WorkDir != job.Record.WorkDir ||
			!strings.Contains(record.SubmitLine, job.Marker) || !strings.Contains(record.SubmitLine, "--chdir="+record.WorkDir) {
			return nil, fmt.Errorf("require completed privacy job with a loaded association, expected %+v observed %+v", job.Record, record)
		}
		job.Record = record
	}
	return jobs, nil
}

func parseAccountingPrivacyJobs(output string) ([]accountingPrivacyJobRecord, error) {
	var records []accountingPrivacyJobRecord
	seen := make(map[string]bool)
	for _, row := range parsePipeRows(output) {
		if len(row) != 8 {
			return nil, fmt.Errorf("parse private accounting job: expected 8 fields, got %q", row)
		}
		id, err := strconv.ParseUint(row[0], 10, 64)
		if err != nil || id == 0 || seen[row[0]] {
			return nil, fmt.Errorf("parse private accounting job: invalid or duplicate JobID %q", row[0])
		}
		seen[row[0]] = true
		records = append(records, accountingPrivacyJobRecord{
			ID: row[0], User: row[1], Account: row[2], Name: row[3], State: row[4],
			AssocID: row[5], WorkDir: row[6], SubmitLine: row[7],
		})
	}
	return records, nil
}

func validateAccountingPrivacyJobs(output string, expected []accountingPrivacyJobRecord) error {
	records, err := parseAccountingPrivacyJobs(output)
	if err != nil {
		return err
	}
	want := make(map[string]accountingPrivacyJobRecord, len(expected))
	for _, record := range expected {
		want[record.ID] = record
	}
	if len(records) != len(expected) {
		return fmt.Errorf("require %d accounting jobs, observed %d:\n%s", len(expected), len(records), output)
	}
	for _, record := range records {
		if record != want[record.ID] {
			return fmt.Errorf("require accounting job %+v, observed %+v", want[record.ID], record)
		}
	}
	return nil
}

func validateAccountingPrivacyRows(output string, fields int, expected []string) error {
	var observed []string
	for _, row := range parsePipeRows(output) {
		if len(row) != fields {
			return fmt.Errorf("parse accounting permissions: expected %d fields, observed %q", fields, row)
		}
		observed = append(observed, strings.Join(row, "|"))
	}
	want := slices.Clone(expected)
	slices.Sort(want)
	slices.Sort(observed)
	if !slices.Equal(want, observed) {
		return fmt.Errorf("require accounting records %q, observed %q", want, observed)
	}
	return nil
}

func parseAccountingPrivacyIdentities(output string) (map[string]accountingPrivacyIdentity, error) {
	identities := make(map[string]accountingPrivacyIdentity)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 7 || fields[0] == "" {
			return nil, fmt.Errorf("parse accounting NSS identity: invalid passwd row %q", line)
		}
		uid, uidErr := strconv.ParseUint(fields[2], 10, 32)
		_, gidErr := strconv.ParseUint(fields[3], 10, 32)
		if uidErr != nil || gidErr != nil || uid < 1000 {
			return nil, fmt.Errorf("require a regular accounting user: invalid UID/GID in %q", line)
		}
		if _, exists := identities[fields[0]]; exists {
			return nil, fmt.Errorf("parse accounting NSS identity: duplicate user %s", fields[0])
		}
		identities[fields[0]] = accountingPrivacyIdentity{UID: fields[2], GID: fields[3]}
	}
	return identities, nil
}

func accountingPrivacyFlags(value *string) string {
	if value == nil || strings.EqualFold(strings.TrimSpace(*value), "none") {
		return ""
	}
	var flags []string
	for _, flag := range strings.Split(strings.ToLower(*value), ",") {
		if flag = strings.TrimSpace(flag); flag != "" {
			flags = append(flags, flag)
		}
	}
	slices.Sort(flags)
	return strings.Join(slices.Compact(flags), ",")
}

func accountingPrivacyConfigFlags(output string) (string, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", nil
	}
	key, value, found := strings.Cut(output, "=")
	if !found || strings.TrimSpace(key) != "PrivateData" || strings.Contains(value, "\n") {
		return "", fmt.Errorf("parse PrivateData from slurmdbd.conf: observed %q", output)
	}
	return accountingPrivacyFlags(&value), nil
}

func accountingPrivacyReadyPod(deployment appsv1.Deployment, pods []corev1.Pod, previousUID types.UID) (corev1.Pod, error) {
	status := deployment.Status
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 ||
		status.ObservedGeneration < deployment.Generation || status.Replicas != 1 ||
		status.UpdatedReplicas != 1 || status.ReadyReplicas != 1 || status.AvailableReplicas != 1 {
		return corev1.Pod{}, fmt.Errorf("wait for accounting Deployment %s generation=%d replicas=%v status=%+v",
			deployment.Name, deployment.Generation, deployment.Spec.Replicas, status)
	}
	var ready []corev1.Pod
	for _, pod := range pods {
		if pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning && kubeobjects.PodReady(pod) {
			ready = append(ready, pod)
		}
	}
	if len(ready) != 1 || ready[0].UID == "" || ready[0].UID == previousUID {
		return corev1.Pod{}, fmt.Errorf("wait for one new Ready accounting pod: previous UID=%s observed=%v", previousUID, accountingPrivacyPodStates(pods))
	}
	return ready[0], nil
}

func accountingPrivacyPodStates(pods []corev1.Pod) []string {
	var ids []string
	for _, pod := range pods {
		ids = append(ids, fmt.Sprintf("%s/%s phase=%s ready=%t deleting=%t", pod.Name, pod.UID,
			pod.Status.Phase, kubeobjects.PodReady(pod), pod.DeletionTimestamp != nil))
	}
	return ids
}
