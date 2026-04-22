package steps

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	enrootMPINCCLFeatureFile = "enroot_mpi_nccl.feature"

	enrootMPINCCLARP          = "all_reduce_perf_mpi -b 8G -e 8G -f 2 -g 1 -N 100"
	enrootMPINCCLTasksPerNode = 8

	enrootMPINCCLJobStartTimeout    = 25 * time.Minute
	enrootMPINCCLJobCompleteTimeout = 45 * time.Minute
	enrootMPINCCLStopTimeout        = 5 * time.Minute
)

type EnrootMPINCCL struct {
	exec framework.Exec

	workers []string
	jobID   string

	failureLogged bool
}

func NewEnrootMPINCCL(exec framework.Exec) *EnrootMPINCCL {
	return &EnrootMPINCCL{exec: exec}
}

func (s *EnrootMPINCCL) Register(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		if path.Base(scenario.Uri) != enrootMPINCCLFeatureFile {
			return ctx, nil
		}
		if err != nil {
			s.logEnrootJobFailureDiagnostics(context.Background(), "scenario failed")
		}

		if cleanupErr := s.cancelCurrentJob(context.Background()); cleanupErr != nil {
			s.exec.Logf("cleanup: cancel enroot mpi/nccl job: %v", cleanupErr)
		}
		return ctx, nil
	})

	sc.Step(`^a finite Enroot MPI/NCCL transfer job is submitted on two GPU workers$`, s.aFiniteEnrootMPINCCLTransferJobIsSubmittedOnTwoGPUWorkers)
	sc.Step(`^the Enroot MPI/NCCL transfer job is running$`, s.theEnrootMPINCCLTransferJobIsRunning)
	sc.Step(`^the Enroot MPI/NCCL transfer job completes successfully$`, s.theEnrootMPINCCLTransferJobCompletesSuccessfully)
}

func (s *EnrootMPINCCL) aFiniteEnrootMPINCCLTransferJobIsSubmittedOnTwoGPUWorkers(ctx context.Context) error {
	if len(s.workers) == 0 {
		workers, err := selectGPUWorkers(ctx, s.exec, 2)
		if err != nil {
			return err
		}
		s.workers = workers
		s.exec.Logf("enroot mpi/nccl: selected workers=%s", strings.Join(workers, ","))
	}

	wrap := fmt.Sprintf("srun --mpi=pmix --container-image=%s --container-mounts=%s %s",
		framework.ShellQuote(enrootDockerImage),
		framework.ShellQuote(enrootDockerMount),
		enrootMPINCCLARP,
	)
	submit := fmt.Sprintf(
		"sbatch --parsable -N 2 --nodelist=%s --gpus-per-node=8 --ntasks-per-node=%d --job-name=e2e-enroot-mpi-transfer --wrap=%s",
		framework.ShellQuote(strings.Join(s.workers, ",")),
		enrootMPINCCLTasksPerNode,
		framework.ShellQuote(wrap),
	)

	out, err := s.exec.ExecJail(ctx, submit)
	if err != nil {
		return fmt.Errorf("submit enroot mpi/nccl transfer job: %w", err)
	}
	jobID, err := parseSbatchJobID(out)
	if err != nil {
		return fmt.Errorf("parse enroot mpi/nccl transfer job id: %w", err)
	}
	s.jobID = jobID
	s.failureLogged = false
	s.exec.Logf("enroot mpi/nccl: submitted job id=%s", jobID)
	return nil
}

func (s *EnrootMPINCCL) theEnrootMPINCCLTransferJobIsRunning(ctx context.Context) error {
	if s.jobID == "" {
		return fmt.Errorf("enroot mpi/nccl job id is empty")
	}
	return waitForJobRunning(ctx, s.exec, s.jobID, enrootMPINCCLJobStartTimeout)
}

func (s *EnrootMPINCCL) theEnrootMPINCCLTransferJobCompletesSuccessfully(ctx context.Context) error {
	if s.jobID == "" {
		return fmt.Errorf("enroot mpi/nccl job id is empty")
	}

	err := s.exec.WaitFor(ctx, fmt.Sprintf("enroot mpi/nccl job %s completed successfully", s.jobID), enrootMPINCCLJobCompleteTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		squeueOutput, err := framework.ExecJailWithDefaultRetry(waitCtx, s.exec, fmt.Sprintf("squeue -h -j %s -o '%%T'", framework.ShellQuote(s.jobID)))
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(squeueOutput) != "" {
			return false, nil
		}

		sacctOutput, err := framework.ExecJailWithDefaultRetry(waitCtx, s.exec, fmt.Sprintf("sacct -X -n -j %s --format=JobIDRaw,State,ExitCode -P || true", framework.ShellQuote(s.jobID)))
		if err != nil {
			return false, err
		}
		state, exitCode := parseSacctJobStateAndExit(sacctOutput, s.jobID)
		if state == "" {
			return false, nil
		}
		if strings.HasPrefix(state, "COMPLETED") && strings.TrimSpace(exitCode) == "0:0" {
			return true, nil
		}
		return false, fmt.Errorf("job %s finished with state=%s exit_code=%s", s.jobID, state, exitCode)
	})
	if err != nil {
		s.logEnrootJobFailureDiagnostics(ctx, "mpi/nccl transfer job did not complete successfully")
		return err
	}

	s.jobID = ""
	return nil
}

func (s *EnrootMPINCCL) cancelCurrentJob(ctx context.Context) error {
	if s.jobID == "" {
		return nil
	}

	jobID := s.jobID
	if err := cancelSlurmJob(ctx, s.exec, jobID, enrootMPINCCLStopTimeout); err != nil {
		return fmt.Errorf("cancel enroot mpi/nccl job %s: %w", jobID, err)
	}
	s.jobID = ""
	return nil
}

func (s *EnrootMPINCCL) logEnrootJobFailureDiagnostics(ctx context.Context, reason string) {
	if s.failureLogged {
		return
	}
	s.failureLogged = true

	if s.jobID == "" {
		s.exec.Logf("enroot mpi/nccl job failed: reason=%s job_id is empty", reason)
		return
	}

	scontrolOutput, err := framework.ExecJailWithDefaultRetry(ctx, s.exec, fmt.Sprintf("scontrol show job %s || true", framework.ShellQuote(s.jobID)))
	if err != nil {
		s.exec.Logf("enroot mpi/nccl job failed: reason=%s job_id=%s scontrol error=%v", reason, s.jobID, err)
		return
	}

	jobState := scontrolField(scontrolOutput, "JobState")
	jobReason := scontrolField(scontrolOutput, "Reason")
	exitCode := scontrolField(scontrolOutput, "ExitCode")
	batchHost := scontrolField(scontrolOutput, "BatchHost")
	stdoutPath := scontrolField(scontrolOutput, "StdOut")
	if stdoutPath == "" {
		stdoutPath = fmt.Sprintf("/slurm-%s.out", s.jobID)
	}

	s.exec.Logf("enroot mpi/nccl job failure: reason=%s job_id=%s state=%s reason_code=%s exit_code=%s batch_host=%s stdout=%s",
		reason, s.jobID, jobState, jobReason, exitCode, batchHost, stdoutPath)

	s.logEnrootJailCommandOutput(ctx, "enroot mpi/nccl debug scontrol show job -d",
		fmt.Sprintf("scontrol show job -d %s || true", framework.ShellQuote(s.jobID)))
	s.logEnrootJailCommandOutput(ctx, "enroot mpi/nccl debug scontrol show step",
		fmt.Sprintf("scontrol show step %s.0 || true", framework.ShellQuote(s.jobID)))
	s.logEnrootJailCommandOutput(ctx, "enroot mpi/nccl debug sacct",
		fmt.Sprintf("sacct -j %s --format=JobID,JobName,NodeList,State,ExitCode,Elapsed,ReqTRES,AllocTRES -P || true", framework.ShellQuote(s.jobID)))
	s.logEnrootJailCommandOutput(ctx, "enroot mpi/nccl debug sstat",
		fmt.Sprintf("sstat -j %s.0 --format=AveCPU,MaxRSS,MaxVMSize -P || true", framework.ShellQuote(s.jobID)))

	failurePattern := fmt.Sprintf("%s|pyxis|enroot|pmix|ucx|mpi|abort|timeout", s.jobID)

	for _, worker := range s.workers {
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug slurmd log excerpt",
			fmt.Sprintf(
				"sudo sh -lc %s",
				framework.ShellQuote(fmt.Sprintf(
					"grep -Ei %s /var/log/slurm/slurmd.log 2>/dev/null | tail -n 200 || true",
					framework.ShellQuote(failurePattern),
				)),
			),
		)
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug pyxis plugstack",
			`sudo sh -lc 'cat /etc/slurm/plugstack.conf 2>/dev/null || true; echo; grep -R "spank_pyxis\|importer=" /etc/slurm/plugstack.conf* 2>/dev/null || true'`)
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug importer script",
			`sudo sh -lc 'ls -lah /opt/slurm_scripts/pyxis_caching_importer.sh 2>/dev/null || true; sha256sum /opt/slurm_scripts/pyxis_caching_importer.sh 2>/dev/null || true; sed -n "1,200p" /opt/slurm_scripts/pyxis_caching_importer.sh 2>/dev/null || true'`)
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug enroot host config",
			`sudo sh -lc 'echo "[host] /etc/enroot/enroot.conf"; sed -n "1,200p" /etc/enroot/enroot.conf 2>/dev/null || true; echo; echo "[host] /etc/enroot/enroot.conf.d"; ls -lah /etc/enroot/enroot.conf.d 2>/dev/null || true; for f in /etc/enroot/enroot.conf.d/*.conf; do [ -f "$f" ] || continue; echo "--- $f"; sed -n "1,200p" "$f" 2>/dev/null || true; done'`)
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug enroot jail config",
			`sudo sh -lc 'echo "[jail] /mnt/jail/etc/enroot/enroot.conf"; sed -n "1,200p" /mnt/jail/etc/enroot/enroot.conf 2>/dev/null || true; echo; echo "[jail] /mnt/jail/etc/enroot/enroot.conf.d"; ls -lah /mnt/jail/etc/enroot/enroot.conf.d 2>/dev/null || true; for f in /mnt/jail/etc/enroot/enroot.conf.d/*.conf; do [ -f "$f" ] || continue; echo "--- $f"; sed -n "1,200p" "$f" 2>/dev/null || true; done'`)
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug path candidates",
			`sudo sh -lc 'for p in /mnt/jail/var/cache/enroot-container-images /var/cache/enroot-container-images /mnt/image-storage/enroot/cache /mnt/jail/mnt/image-storage/enroot/cache /mnt/image-storage/enroot/data /mnt/jail/mnt/image-storage/enroot/data; do echo "=== $p ==="; if [ -d "$p" ]; then files=$(find "$p" -maxdepth 2 -type f 2>/dev/null | wc -l); sqsh=$(find "$p" -maxdepth 6 -type f -name "*.sqsh" 2>/dev/null | wc -l); echo "files(maxdepth2)=$files sqsh(maxdepth6)=$sqsh"; find "$p" -maxdepth 6 -type f -name "*.sqsh" 2>/dev/null | sort | head -n 50; else echo "missing"; fi; echo; done'`)
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug pyxis runtime ls", "sudo sh -lc 'ls -lah /run/pyxis || true'")
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug pyxis runtime files", "sudo sh -lc 'find /run/pyxis -maxdepth 3 -type f -print 2>/dev/null || true'")
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug enroot cache tree", "sudo sh -lc 'tree -L 3 /mnt/image-storage/enroot/cache/ 2>/dev/null || true'")
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug enroot data tree", "sudo sh -lc 'tree -L 3 /mnt/image-storage/enroot/data/ 2>/dev/null || true'")
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug enroot squashfs files",
			fmt.Sprintf("sudo find %s -maxdepth 6 -type f -name '*%s' -ls 2>/dev/null || true",
				framework.ShellQuote(enrootSquashRoot), enrootSquashPattern))
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug checks output",
			`sudo sh -lc 'ls -lah /mnt/jail/opt/soperator-outputs/slurm_scripts 2>/dev/null || true; tail -n 120 /mnt/jail/opt/soperator-outputs/slurm_scripts/*cleanup_enroot* 2>/dev/null || true; tail -n 120 /mnt/jail/opt/soperator-outputs/slurm_scripts/*chmod_enroot_layers* 2>/dev/null || true'`)
		s.logEnrootWorkerCommandOutput(ctx, worker, "enroot mpi/nccl debug coredumpctl",
			fmt.Sprintf(
				"sudo sh -lc %s",
				framework.ShellQuote(fmt.Sprintf(
					"command -v coredumpctl >/dev/null && coredumpctl --no-pager --since %s | tail -n 50 || true",
					framework.ShellQuote("2 hours ago"),
				)),
			),
		)
	}

	if batchHost == "" {
		return
	}

	out, err := runWorkerCommandWithDefaultRetry(ctx, s.exec, batchHost,
		fmt.Sprintf("sudo sh -lc %s", framework.ShellQuote(fmt.Sprintf("tail -n 200 %s 2>/dev/null || echo 'slurm output not found: %s'", framework.ShellQuote(stdoutPath), stdoutPath))))
	if err != nil {
		s.exec.Logf("enroot mpi/nccl job failure stdout tail on %s failed: %v", batchHost, err)
		return
	}
	s.exec.Logf("enroot mpi/nccl job failure stdout tail on %s:\n%s", batchHost, strings.TrimSpace(out))
}

func (s *EnrootMPINCCL) logEnrootJailCommandOutput(ctx context.Context, label, command string) {
	output, err := s.exec.ExecJailWithRetry(ctx, command, 2, 5*time.Second)
	if err != nil {
		s.exec.Logf("%s failed: %v", label, err)
		return
	}
	s.exec.Logf("%s output:\n%s", label, strings.TrimSpace(output))
}

func (s *EnrootMPINCCL) logEnrootWorkerCommandOutput(ctx context.Context, worker, label, command string) {
	output, err := runWorkerCommandWithDefaultRetry(ctx, s.exec, worker, command)
	if err != nil {
		s.exec.Logf("%s on %s failed: %v", label, worker, err)
		return
	}
	s.exec.Logf("%s on %s output:\n%s", label, worker, strings.TrimSpace(output))
}

func parseSacctJobStateAndExit(sacctOutput, jobID string) (state string, exitCode string) {
	for _, line := range strings.Split(strings.TrimSpace(sacctOutput), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		fields := strings.Split(trimmed, "|")
		if len(fields) < 3 {
			continue
		}
		if strings.TrimSpace(fields[0]) != jobID {
			continue
		}
		return strings.TrimSpace(fields[1]), strings.TrimSpace(fields[2])
	}
	return "", ""
}
