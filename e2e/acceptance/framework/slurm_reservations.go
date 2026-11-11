package framework

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// A lease outlives the longest scenario and its bounded jobs, but expires if
// the runner disappears. STATIC_ALLOC prevents drain/replacement from moving it.
const reservationDuration = "08:00:00"
const reservationJobTimeLimit = "02:00:00"

func (s *SlurmClient) reservations(ctx context.Context) (map[string][]string, error) {
	output, err := s.runtime.Controller().Run(ctx, "scontrol -o show reservations")
	if err != nil {
		return nil, fmt.Errorf("list Slurm reservations: %w", err)
	}
	result := make(map[string][]string)
	for _, line := range strings.Split(output, "\n") {
		fields := make(map[string]string)
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if ok {
				fields[key] = value
			}
		}
		name := fields["ReservationName"]
		if name == "" {
			continue
		}
		nodes := fields["Nodes"]
		if nodes == "" || nodes == "(null)" {
			result[name] = nil
			continue
		}
		expanded, err := s.runtime.Controller().Run(ctx, "scontrol show hostnames "+ShellQuote(nodes))
		if err != nil {
			return nil, fmt.Errorf("expand reservation %s workers: %w", name, err)
		}
		result[name] = ParseSlurmNodeNames(expanded)
	}
	return result, nil
}

func (s *SlurmClient) createReservation(ctx context.Context, name string, nodes []string) error {
	command := "scontrol create reservation ReservationName=" + ShellQuote(name) +
		" StartTime=now Duration=" + reservationDuration + " Users=root Flags=STATIC_ALLOC Nodes=" + ShellQuote(strings.Join(nodes, ","))
	output, err := s.runtime.Controller().Run(ctx, command)
	if err != nil {
		return fmt.Errorf("create Slurm reservation %s: %w; output: %s", name, err, output)
	}
	return nil
}

func reservationContention(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "requested nodes are busy") ||
		strings.Contains(message, "requested node configuration is not available") ||
		strings.Contains(message, "reservation overlaps")
}

func (s *SlurmClient) deleteReservation(ctx context.Context, name string) error {
	output, err := s.runtime.Controller().Run(ctx, "scontrol delete ReservationName="+ShellQuote(name))
	observed, checkErr := s.reservations(ctx)
	if checkErr != nil {
		return errors.Join(err, fmt.Errorf("confirm reservation removal %s: %w", name, checkErr))
	}
	if _, exists := observed[name]; !exists {
		return nil
	}
	return fmt.Errorf("remove reservation %s: %w; output: %s", name, errors.Join(err, errors.New("reservation still exists")), output)
}

func (s *SlurmClient) reservationJobs(ctx context.Context, name string) ([]string, error) {
	output, err := s.runtime.Controller().Run(ctx, "squeue -h --reservation="+ShellQuote(name)+" -o '%i'")
	if err != nil {
		return nil, fmt.Errorf("query reservation %s jobs: %w", name, err)
	}
	return strings.Fields(output), nil
}

func (s *SlurmClient) cancelReservationJobs(ctx context.Context, name string) error {
	_, err := s.runtime.Controller().Run(ctx, "scancel --reservation="+ShellQuote(name))
	return err
}

func (s *SlurmClient) addReservationUser(ctx context.Context, name, user string) error {
	if user == "" || user == "root" {
		return nil
	}
	// User names are values in Slurm's comma-separated ACL, not shell fragments.
	if strings.ContainsAny(user, ",=+ \t\r\n") || strings.HasPrefix(user, "-") {
		return fmt.Errorf("grant reservation access: invalid user %q", user)
	}
	_, err := s.runtime.Controller().Run(ctx, "scontrol update ReservationName="+ShellQuote(name)+" Users+="+ShellQuote(user))
	if err != nil {
		return fmt.Errorf("grant user %s access to reservation %s: %w", user, name, err)
	}
	return nil
}

// WorkerReservationJobFlags also supports commands crossing sudo -iu, which
// intentionally drops the jail command's reservation environment.
func WorkerReservationJobFlags(ctx context.Context, user string) ([]string, error) {
	r := workerReservation(ctx)
	if r == nil {
		return nil, nil
	}
	if r.backend == nil || len(r.workers) == 0 {
		return nil, fmt.Errorf("submit reserved job: reserve workers first")
	}
	if err := r.backend.addReservationUser(ctx, r.id, user); err != nil {
		return nil, err
	}
	return []string{"--time=" + reservationJobTimeLimit, "--reservation=" + r.id}, nil
}
