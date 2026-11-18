package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

// nodeReservationCommand reserves the named nodes whole for users in the main partition, starting now:
// every core, GPU and byte of memory, so that no job outside the reservation can share the workers.
func nodeReservationCommand(name, users string, nodes []string, duration time.Duration, flags ...string) string {
	args := []string{"scontrol", "create", "reservation",
		"ReservationName=" + name,
		"Users=" + users,
		"PartitionName=" + framework.MainPartitionName,
		"StartTime=now",
		fmt.Sprintf("Duration=%d", int(duration.Minutes())),
		"Nodes=" + strings.Join(nodes, ","),
	}
	if len(flags) > 0 {
		args = append(args, "Flags="+strings.Join(flags, ","))
	}
	return framework.ShellJoin(args...)
}

// deleteReservationCommand succeeds when the reservation is deleted or already gone,
// so a retry after a lost exec stream does not fail on the missing reservation.
func deleteReservationCommand(name string) string {
	quoted := framework.ShellQuote(name)
	return "scontrol delete reservation " + quoted + " || ! scontrol show reservation " + quoted + " --oneliner >/dev/null 2>&1"
}

// reservationsByName maps each `scontrol show reservation --oneliner` line to its name.
func reservationsByName(output string) map[string]string {
	shown := map[string]string{}
	for line := range strings.SplitSeq(output, "\n") {
		if name := extractField(line, "ReservationName"); name != "" {
			shown[name] = line
		}
	}
	return shown
}

// reservationNamesWithPrefix returns the names of the reservations starting with prefix
// in `scontrol show reservations --oneliner` output.
func reservationNamesWithPrefix(output, prefix string) []string {
	var names []string
	for line := range strings.SplitSeq(output, "\n") {
		if name := extractField(line, "ReservationName"); strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names
}

// deleteStaleReservations removes the reservations with the prefix left by a run that lost its cleanup;
// they would keep the nodes from being idle.
func deleteStaleReservations(ctx context.Context, runtime framework.Runtime, prefix string) error {
	output, err := runtime.Jail().RunWithDefaultRetry(ctx, "scontrol show reservations --oneliner 2>/dev/null || true")
	if err != nil {
		return fmt.Errorf("list reservations: %w", err)
	}
	for _, name := range reservationNamesWithPrefix(output, prefix) {
		runtime.Logf("Acceptance: deleting stale reservation %s", name)
		if _, err := runtime.Jail().RunWithDefaultRetry(ctx, deleteReservationCommand(name)); err != nil {
			return fmt.Errorf("delete stale reservation %s: %w", name, err)
		}
	}
	return nil
}
