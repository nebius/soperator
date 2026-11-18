package framework

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// SlurmCommandOptions separates allocation arguments from the executable payload.
// Args are self-contained option argv entries (--mem=128M, -c1), never
// positional payload or shell fragments. JobID explicitly selects an existing
// allocation for srun; a reserved step must belong to the same reservation.
type SlurmCommandOptions struct {
	Args      []string
	Nodelist  []string
	Nodes     int
	TimeLimit time.Duration
	RunAsUser string
	JobID     string
}

func quoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = ShellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func loginCommand(user string, args []string) string {
	if user != "" {
		args = append([]string{"sudo", "-iu", user, "--"}, args...)
	}
	return quoteArgs(args)
}

func (r *workerReservation) grantUser(ctx context.Context, user string) error {
	if strings.ContainsAny(user, ",=+ \t\r\n") || strings.HasPrefix(user, "-") {
		return fmt.Errorf("grant reservation access: invalid user %q", user)
	}
	r.userMu.Lock()
	defer r.userMu.Unlock()
	if r.users[user] {
		return nil
	}
	if err := r.backend.grantUser(ctx, r.id, user); err != nil {
		return err
	}
	r.users[user] = true
	return nil
}

func (r *workerReservation) jobFlags(ctx context.Context, user string, limit time.Duration) ([]string, error) {
	if err := r.grantUser(ctx, user); err != nil {
		return nil, err
	}
	remaining := time.Until(r.expires) - r.options.JobDrainTimeout - r.options.RecoveryTimeout - reservationOperationTimeout
	if limit < 0 {
		return nil, fmt.Errorf("submit reserved job: time limit must be positive")
	}
	if limit == 0 {
		limit = min(r.options.JobTimeLimit, remaining)
		if deadline, ok := ctx.Deadline(); ok {
			limit = min(limit, time.Until(deadline))
		}
	}
	if limit <= 0 || limit > remaining {
		return nil, fmt.Errorf("submit reserved job: time limit %s exceeds usable lease lifetime %s", limit, remaining)
	}
	return []string{"--reservation=" + r.id, "--time=" + slurmDuration(limit)}, nil
}

// validateAllocationArgs keeps the allocation/payload boundary structural without
// maintaining Slurm's option arities or rejecting ordinary abbreviations/bundles.
func validateAllocationArgs(args []string) error {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") || arg == "-" || arg == "--" {
			return fmt.Errorf("build Slurm allocation: use self-contained option argv entries (for example --mem=128M or -c1); put payload separately: %q", arg)
		}
	}
	return nil
}

// reservedNodeCount ties the requested node count to the selected workers so
// Slurm cannot expand a targeted nodelist onto other reserved workers.
func reservedNodeCount(ctx context.Context, names []string, count int) (int, error) {
	if err := requireReservedWorkers(ctx, names); err != nil {
		return 0, err
	}
	r := workerReservationFromContext(ctx)
	if r == nil {
		return count, nil
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			return 0, fmt.Errorf("submit reserved job: duplicate worker %q", name)
		}
		seen[name] = true
	}
	if count < 0 {
		return 0, fmt.Errorf("submit reserved job: node count must not be negative")
	}
	if len(names) > 0 {
		if count != 0 && count != len(names) {
			return 0, fmt.Errorf("submit reserved job: node count must match the nodelist")
		}
		count = len(names)
	}
	r.mu.Lock()
	allocated := len(r.workers)
	r.mu.Unlock()
	if count > allocated {
		return 0, fmt.Errorf("submit reserved job: node count exceeds reservation")
	}
	return count, nil
}

// validateBoundArgs protects the options this layer owns, not Slurm's general
// grammar. Other argv entries are interpreted by Slurm. Controlled options are
// appended last; payload and heterogeneous-component boundaries are separate.
func validateBoundArgs(args []string, selected bool) error {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		// -Z has no argument and can be bundled. Conservatively reject ambiguous
		// short entries containing Z; attached values can use the long option form.
		noAllocate := len(name) > 2 && strings.HasPrefix("--no-allocate", name)
		shortNoAllocate := strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "Z")
		if noAllocate || shortNoAllocate {
			return fmt.Errorf("submit reserved job: --no-allocate/-Z bypasses reservation ownership: %q", arg)
		}
		if len(name) > 2 && strings.HasPrefix("--reservation", name) {
			return fmt.Errorf("submit reserved job: reservation binding is managed by the scope")
		}
		if selected && (name == "--nodefile" || strings.HasPrefix(arg, "-F")) {
			return fmt.Errorf("submit reserved job: nodefile overrides selected workers")
		}
	}
	return nil
}

type reservationBinding struct {
	flags []string
	nodes int
	done  func()
}

func bindReservation(ctx context.Context, args, names []string, nodes int, user string, limit time.Duration) (reservationBinding, error) {
	var binding reservationBinding
	if err := validateAllocationArgs(args); err != nil {
		return binding, err
	}
	count, err := reservedNodeCount(ctx, names, nodes)
	if err != nil {
		return binding, err
	}
	binding.nodes, binding.done = count, func() {}
	if r := workerReservationFromContext(ctx); r != nil {
		if err := validateBoundArgs(args, len(names) > 0); err != nil {
			return binding, err
		}
		done, err := r.beginSubmission()
		if err != nil {
			return binding, err
		}
		flags, err := r.jobFlags(ctx, user, limit)
		if err != nil {
			done()
			return binding, err
		}
		binding.flags, binding.done = flags, done
	}
	return binding, nil
}

// RunSlurm runs one srun/salloc allocation, or an explicitly selected srun step.
// In a reservation scope it admits the operation until execution returns, grants
// the submitting user's access and installs binding after sudo's login transition.
func (s *SlurmClient) RunSlurm(ctx context.Context, command string, options SlurmCommandOptions, payload ...string) (string, error) {
	if command != "srun" && command != "salloc" {
		return "", fmt.Errorf("run Slurm command: expected srun or salloc")
	}
	if len(payload) == 0 {
		return "", fmt.Errorf("run Slurm command: executable is required")
	}
	binding, err := bindReservation(ctx, options.Args, options.Nodelist, options.Nodes, options.RunAsUser, options.TimeLimit)
	if err != nil {
		return "", err
	}
	defer binding.done()
	options.Nodes = binding.nodes
	args := append(slices.Clone(options.Args), binding.flags...)
	r := workerReservationFromContext(ctx)
	if r != nil {
		if options.JobID != "" {
			if command != "srun" {
				return "", fmt.Errorf("run reserved step: job ID requires srun")
			}
			jobs, err := r.backend.jobs(ctx, r.id)
			if err != nil {
				return "", err
			}
			if !slices.Contains(jobs, options.JobID) {
				return "", fmt.Errorf("run reserved step: job %s is outside reservation %s", options.JobID, r.id)
			}
		}
	}
	if options.Nodes > 0 {
		args = append(args, fmt.Sprintf("--nodes=%d", options.Nodes))
	}
	if len(options.Nodelist) > 0 {
		args = append(args, "--nodelist="+strings.Join(options.Nodelist, ","))
	}
	if options.JobID != "" {
		args = append(args, "--jobid="+options.JobID)
	}
	if r == nil && options.TimeLimit > 0 {
		args = append(args, "--time="+slurmDuration(options.TimeLimit))
	}
	invocation := []string{"env"}
	if r != nil && options.JobID == "" {
		// srun otherwise inherits an unrelated enclosing allocation, even when a
		// reservation argument is supplied. Explicit steps are validated above.
		invocation = append(invocation, "-u", "SLURM_JOB_ID", "-u", "SLURM_JOBID", "-u", "SLURM_HET_SIZE")
		for _, arg := range options.Args {
			name, _, _ := strings.Cut(arg, "=")
			if len(name) > 2 && strings.HasPrefix("--jobid", name) {
				return "", fmt.Errorf("run reserved command: use JobID for an existing allocation")
			}
		}
	}
	if r != nil {
		invocation = append(invocation, "SLURM_RESERVATION="+r.id, "SALLOC_RESERVATION="+r.id)
	}
	invocation = append(invocation, command)
	invocation = append(invocation, args...)
	invocation = append(invocation, "--")
	invocation = append(invocation, payload...)
	return s.runtime.Jail().Run(ctx, loginCommand(options.RunAsUser, invocation))
}

// RunReservationUtility supplies Slurm's reservation/time environment to a known
// utility after an optional login-user switch. Utilities with explicit allocation
// flags must use an adapter that supplies the same binding at submission; CLI
// options override environment. This does not rewrite arbitrary shell scripts.
func (s *SlurmClient) RunReservationUtility(ctx context.Context, user string, argv ...string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("run reservation utility: executable is required")
	}
	binding, err := bindReservation(ctx, nil, nil, 0, user, 0)
	if err != nil {
		return "", err
	}
	defer binding.done()
	args := []string{"env"}
	if r := workerReservationFromContext(ctx); r != nil {
		limit := strings.TrimPrefix(binding.flags[1], "--time=")
		args = append(args, "-u", "SLURM_JOB_ID", "-u", "SLURM_JOBID", "-u", "SLURM_HET_SIZE",
			"SBATCH_RESERVATION="+r.id, "SLURM_RESERVATION="+r.id, "SALLOC_RESERVATION="+r.id,
			"SBATCH_TIMELIMIT="+limit, "SLURM_TIMELIMIT="+limit, "SALLOC_TIMELIMIT="+limit)
	}
	args = append(args, argv...)
	return s.runtime.Jail().Run(ctx, loginCommand(user, args))
}

// WithWorkerReservationJobFlags holds submission admission while an adapter
// executes with bound flags across its own sudo or remote boundary. The callback
// must await submissions and stop producers before returning.
func WithWorkerReservationJobFlags(ctx context.Context, user string, run func([]string) error) error {
	if run == nil {
		return fmt.Errorf("run reservation adapter: callback is required")
	}
	binding, err := bindReservation(ctx, nil, nil, 0, user, 0)
	if err != nil {
		return err
	}
	defer binding.done()
	return run(binding.flags)
}
