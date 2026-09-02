package sharedsteps

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	accountingTestUser       = "bob"
	accountingTestUserHome   = "/home/bob"
	accountingTestAccount    = "e2e-research"
	accountingTestJobName    = "e2e-accounting"
	accountingSmokeTimeout   = 2 * time.Minute
	accountingCleanupTimeout = 2 * time.Minute
)

var accountingJobFields = []string{
	"JobIDRaw",
	"JobName",
	"User",
	"Account",
	"State",
	"ExitCode",
	"Elapsed",
	"ElapsedRaw",
	"NNodes",
	"NCPUS",
	"AllocTRES",
	"Start",
	"End",
}

type Accounting struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	clusterName string
	worker      framework.WorkerInfo
	job         framework.SbatchJob
}

type accountingClusterRecord struct {
	Cluster     string
	ControlHost string
	ControlPort string
}

type accountingJobRecord struct {
	JobID      string
	JobName    string
	User       string
	Account    string
	State      string
	ExitCode   string
	Elapsed    string
	ElapsedRaw string
	Nodes      string
	CPUs       string
	AllocTRES  string
	Start      string
	End        string
}

func NewAccounting(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *Accounting {
	return &Accounting{
		info:     info,
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *Accounting) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^Slurm accounting is reachable and the cluster is registered$`, s.slurmAccountingIsReachable)
	sc.Step(`^an acceptance test user exists$`, s.acceptanceTestUserExists)
	sc.Step(`^a test Slurm account is created and associated with the user$`, s.createTestAccountAndAssociation)
	sc.Step(`^the user association is visible in Slurm accounting$`, s.userAssociationIsVisible)
	sc.Step(`^the user submits a one-node smoke job to the test account$`, s.submitAccountingSmokeJob)
	sc.Step(`^the job completes and is recorded with the expected user, account, and allocated resources$`, s.jobIsRecorded)
}

func (s *Accounting) CleanupAndReset(ctx context.Context) {
	if !s.job.IsZero() {
		cleanupCtx, cancel := context.WithTimeout(ctx, accountingCleanupTimeout)
		defer cancel()
		if err := s.slurm.CancelJob(cleanupCtx, s.job.ID, accountingCleanupTimeout); err != nil {
			s.runtime.Logf("cleanup: cancel accounting job %s: %v", s.job.ID, err)
		}
	}
	s.clusterName = ""
	s.worker = framework.WorkerInfo{}
	s.job = framework.SbatchJob{}
}

func (s *Accounting) slurmAccountingIsReachable(ctx context.Context) error {
	cluster, err := s.kubectl.SlurmCluster(ctx, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	if !cluster.AccountingEnabled {
		s.runtime.Logf("acceptance: Slurm accounting is disabled, skipping scenario")
		return godog.ErrSkip
	}

	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx,
		"sacctmgr show cluster format=Cluster,ControlHost,ControlPort -nP")
	if err != nil {
		return fmt.Errorf("query registered Slurm clusters: %w", err)
	}
	record, err := findAccountingCluster(output, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	s.clusterName = record.Cluster
	s.runtime.Logf("accounting: cluster=%s control=%s:%s",
		record.Cluster, record.ControlHost, record.ControlPort)
	return nil
}

func (s *Accounting) acceptanceTestUserExists(ctx context.Context) error {
	workers, err := s.selector.PickWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	if err := ensureSSHTestUser(ctx, s.runtime, accountingTestUser); err != nil {
		return err
	}
	return waitForSSHTestUserOnWorker(ctx, s.runtime, accountingTestUser, s.worker)
}

func (s *Accounting) createTestAccountAndAssociation(ctx context.Context) error {
	if s.clusterName == "" {
		return fmt.Errorf("registered accounting cluster is not selected")
	}

	cluster := framework.ShellQuote(s.clusterName)
	account := framework.ShellQuote(accountingTestAccount)
	accountExists, err := s.slurmAccountingAccountExists(ctx)
	if err != nil {
		return err
	}
	if !accountExists {
		if _, err := s.runtime.Jail().Run(ctx, fmt.Sprintf(
			"sacctmgr -i add account name=%s cluster=%s description=%s organization=%s",
			account,
			cluster,
			framework.ShellQuote("Soperator acceptance tests"),
			framework.ShellQuote("e2e"),
		)); err != nil {
			return fmt.Errorf("create Slurm account %s: %w", accountingTestAccount, err)
		}
	}

	associationExists, _, err := s.queryTestAssociation(ctx)
	if err != nil {
		return err
	}
	if associationExists {
		return nil
	}

	userExists, err := s.slurmAccountingUserExists(ctx)
	if err != nil {
		return err
	}
	command := fmt.Sprintf(
		"sacctmgr -i add user name=%s account=%s cluster=%s",
		framework.ShellQuote(accountingTestUser), account, cluster,
	)
	if !userExists {
		command += " defaultaccount=" + account
	}
	if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
		return fmt.Errorf("associate Slurm user %s with account %s: %w",
			accountingTestUser, accountingTestAccount, err)
	}
	return nil
}

func (s *Accounting) slurmAccountingAccountExists(ctx context.Context) (bool, error) {
	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf(
		"sacctmgr show account where name=%s format=Account -nP",
		framework.ShellQuote(accountingTestAccount),
	))
	if err != nil {
		return false, fmt.Errorf("query Slurm account %s: %w", accountingTestAccount, err)
	}
	for _, row := range parseAccountingRows(output) {
		if len(row) > 0 && row[0] == accountingTestAccount {
			return true, nil
		}
	}
	return false, nil
}

func (s *Accounting) slurmAccountingUserExists(ctx context.Context) (bool, error) {
	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf(
		"sacctmgr show user where name=%s format=User -nP",
		framework.ShellQuote(accountingTestUser),
	))
	if err != nil {
		return false, fmt.Errorf("query Slurm user %s: %w", accountingTestUser, err)
	}
	for _, row := range parseAccountingRows(output) {
		if len(row) > 0 && row[0] == accountingTestUser {
			return true, nil
		}
	}
	return false, nil
}

func (s *Accounting) userAssociationIsVisible(ctx context.Context) error {
	exists, output, err := s.queryTestAssociation(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Slurm association cluster=%s account=%s user=%s is missing from output:\n%s",
			s.clusterName, accountingTestAccount, accountingTestUser, strings.TrimSpace(output))
	}
	return nil
}

func (s *Accounting) queryTestAssociation(ctx context.Context) (bool, string, error) {
	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf(
		"sacctmgr show assoc where cluster=%s account=%s user=%s format=Cluster,Account,User -nP",
		framework.ShellQuote(s.clusterName),
		framework.ShellQuote(accountingTestAccount),
		framework.ShellQuote(accountingTestUser),
	))
	if err != nil {
		return false, output, fmt.Errorf("query Slurm association for %s/%s: %w",
			accountingTestAccount, accountingTestUser, err)
	}
	return accountingAssociationExists(
		output, s.clusterName, accountingTestAccount, accountingTestUser,
	), output, nil
}

func (s *Accounting) submitAccountingSmokeJob(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker for accounting smoke job is not selected")
	}
	stdoutPath := accountingTestUserHome + "/e2e-accounting.out"
	stderrPath := accountingTestUserHome + "/e2e-accounting.err"
	command := strings.Join([]string{
		"sudo", "-iu", framework.ShellQuote(accountingTestUser), "--", "sbatch",
		"--parsable",
		"--job-name=" + framework.ShellQuote(accountingTestJobName),
		"--account=" + framework.ShellQuote(accountingTestAccount),
		"-N", "1",
		"--nodelist=" + framework.ShellQuote(s.worker.Name),
		"--output=" + framework.ShellQuote(stdoutPath),
		"--error=" + framework.ShellQuote(stderrPath),
		"--open-mode=truncate",
		"--wrap=" + framework.ShellQuote("hostname; sleep 2"),
	}, " ")
	output, err := s.runtime.Jail().Run(ctx, command)
	if err != nil {
		return fmt.Errorf("submit accounting smoke job as %s: %w", accountingTestUser, err)
	}
	jobID, err := framework.ParseSbatchJobID(output)
	if err != nil {
		return fmt.Errorf("parse accounting smoke job ID: %w", err)
	}
	s.job = framework.SbatchJob{
		ID:         jobID,
		JobName:    accountingTestJobName,
		StdoutPath: stdoutPath,
		StderrPath: stderrPath,
	}
	s.runtime.Logf("accounting: submitted job=%s user=%s account=%s",
		jobID, accountingTestUser, accountingTestAccount)
	return nil
}

func (s *Accounting) jobIsRecorded(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("accounting smoke job was not submitted")
	}
	if err := waitForJobSucceeded(ctx, s.runtime, s.slurm, s.job, accountingSmokeTimeout); err != nil {
		return err
	}

	return s.runtime.WaitFor(ctx, fmt.Sprintf("job %s accounting record", s.job.ID),
		accountingSmokeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			output, err := s.runtime.Jail().RunWithDefaultRetry(waitCtx, fmt.Sprintf(
				"sacct -X -j %s -nP --format=%s",
				framework.ShellQuote(s.job.ID), strings.Join(accountingJobFields, ","),
			))
			if err != nil {
				return false, fmt.Errorf("query accounting record for job %s: %w", s.job.ID, err)
			}
			record, found := findAccountingJobRecord(output, s.job.ID)
			if !found {
				return false, fmt.Errorf("job %s is missing from sacct output:\n%s",
					s.job.ID, strings.TrimSpace(output))
			}
			if err := validateAccountingJobRecord(record); err != nil {
				return false, err
			}
			return true, nil
		})
}

func findAccountingCluster(output, expectedCluster string) (accountingClusterRecord, error) {
	for _, row := range parseAccountingRows(output) {
		if len(row) < 3 || row[0] != expectedCluster {
			continue
		}
		record := accountingClusterRecord{
			Cluster:     row[0],
			ControlHost: row[1],
			ControlPort: row[2],
		}
		if record.ControlHost == "" || record.ControlPort == "" {
			return accountingClusterRecord{}, fmt.Errorf(
				"registered Slurm cluster %s has incomplete control endpoint host=%q port=%q",
				record.Cluster, record.ControlHost, record.ControlPort)
		}
		return record, nil
	}
	return accountingClusterRecord{}, fmt.Errorf(
		"Slurm cluster %s is not registered in accounting output:\n%s",
		expectedCluster, strings.TrimSpace(output))
}

func accountingAssociationExists(output, cluster, account, user string) bool {
	for _, row := range parseAccountingRows(output) {
		if len(row) >= 3 && row[0] == cluster && row[1] == account && row[2] == user {
			return true
		}
	}
	return false
}

func findAccountingJobRecord(output, jobID string) (accountingJobRecord, bool) {
	for _, row := range parseAccountingRows(output) {
		if len(row) < len(accountingJobFields) || row[0] != jobID {
			continue
		}
		return accountingJobRecord{
			JobID:      row[0],
			JobName:    row[1],
			User:       row[2],
			Account:    row[3],
			State:      row[4],
			ExitCode:   row[5],
			Elapsed:    row[6],
			ElapsedRaw: row[7],
			Nodes:      row[8],
			CPUs:       row[9],
			AllocTRES:  row[10],
			Start:      row[11],
			End:        row[12],
		}, true
	}
	return accountingJobRecord{}, false
}

func validateAccountingJobRecord(record accountingJobRecord) error {
	var problems []string
	for _, field := range []struct {
		name     string
		actual   string
		expected string
	}{
		{"job name", record.JobName, accountingTestJobName},
		{"user", record.User, accountingTestUser},
		{"account", record.Account, accountingTestAccount},
		{"state", record.State, "COMPLETED"},
		{"exit code", record.ExitCode, "0:0"},
	} {
		if field.actual != field.expected {
			problems = append(problems, fmt.Sprintf("%s=%q, expected %q",
				field.name, field.actual, field.expected))
		}
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"elapsed", record.Elapsed},
		{"start", record.Start},
		{"end", record.End},
	} {
		if field.value == "" || field.value == "Unknown" {
			problems = append(problems, fmt.Sprintf("%s=%q", field.name, field.value))
		}
	}
	if record.Nodes != "1" {
		problems = append(problems, fmt.Sprintf("nodes=%q, expected %q", record.Nodes, "1"))
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"elapsed raw", record.ElapsedRaw},
		{"CPUs", record.CPUs},
		{"allocated CPU TRES", accountingTRESValue(record.AllocTRES, "cpu")},
		{"allocated billing TRES", accountingTRESValue(record.AllocTRES, "billing")},
	} {
		value, err := strconv.ParseFloat(field.value, 64)
		if err != nil || value <= 0 {
			problems = append(problems, fmt.Sprintf("%s=%q, expected a positive number", field.name, field.value))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("job %s has invalid accounting record: %s",
			record.JobID, strings.Join(problems, "; "))
	}
	return nil
}

func accountingTRESValue(allocTRES, name string) string {
	for _, tres := range strings.Split(allocTRES, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(tres), "=")
		if found && key == name {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseAccountingRows(output string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) > 0 && fields[len(fields)-1] == "" {
			fields = fields[:len(fields)-1]
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		rows = append(rows, fields)
	}
	return rows
}
