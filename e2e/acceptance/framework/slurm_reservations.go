package framework

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

type slurmReservation struct {
	Name     string
	Nodes    []string
	Flags    []string
	Users    []string
	EndTime  string
	Duration string
	State    string
}

var errReservationAbsent = errors.New("reservation is absent")

type reservationBackend interface {
	list(ctx context.Context) ([]slurmReservation, error)
	get(ctx context.Context, name string) (*slurmReservation, error)
	create(ctx context.Context, name string, nodes []string, lifetime time.Duration) error
	delete(ctx context.Context, name string) error
	jobs(ctx context.Context, name string) ([]string, error)
	cancelJobs(ctx context.Context, name string) error
	grantUser(ctx context.Context, name, user string) error
	powerTimeout(ctx context.Context, flag string) (time.Duration, error)
}

type slurmReservations struct {
	runtime Runtime
	hosts   *readCache[[]string]
}

func (s *slurmReservations) read(ctx context.Context, command string) (string, error) {
	return discoveryRead(ctx, func(ctx context.Context) (string, error) { return s.runtime.Controller().Run(ctx, command) })
}

func (s *slurmReservations) parse(ctx context.Context, output string) ([]slurmReservation, error) {
	var result []slurmReservation
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" || strings.TrimSpace(line) == "No reservations in the system" {
			continue
		}
		fields := make(map[string]string)
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if ok {
				fields[key] = value
			}
		}
		name, hosts := fields["ReservationName"], fields["Nodes"]
		if name == "" || hosts == "" {
			return nil, fmt.Errorf("parse Slurm reservation record %q", line)
		}
		var nodes []string
		if hosts != "(null)" {
			var err error
			nodes, err = s.hosts.get(ctx, hosts, 0, func(ctx context.Context) ([]string, error) {
				names, err := NewSlurmClient(s.runtime).ExpandHostnames(ctx, hosts)
				if err != nil {
					return nil, err
				}
				slices.Sort(names)
				return names, nil
			})
			if err != nil {
				return nil, fmt.Errorf("expand reservation %s: %w", name, err)
			}
		}
		result = append(result, slurmReservation{Name: name, Nodes: slices.Clone(nodes), Flags: strings.Split(fields["Flags"], ","), Users: strings.Split(fields["Users"], ","), EndTime: fields["EndTime"], Duration: fields["Duration"], State: fields["State"]})
	}
	return result, nil
}

func (s *slurmReservations) list(ctx context.Context) ([]slurmReservation, error) {
	out, err := s.read(ctx, "scontrol -o show reservations")
	if err != nil {
		return nil, fmt.Errorf("list Slurm reservations: %w", err)
	}
	return s.parse(ctx, out)
}

func (s *slurmReservations) get(ctx context.Context, name string) (*slurmReservation, error) {
	out, err := s.read(ctx, "scontrol -o show reservation "+ShellQuote(name))
	if err != nil {
		message := strings.ToLower(out + " " + err.Error())
		if strings.Contains(message, "invalid reservation") || strings.Contains(message, "reservation not found") || strings.Contains(message, "reservation "+strings.ToLower(name)+" not found") {
			return nil, fmt.Errorf("%w: %s", errReservationAbsent, name)
		}
		return nil, fmt.Errorf("read Slurm reservation %s: %w", name, err)
	}
	records, err := s.parse(ctx, out)
	if err != nil {
		return nil, err
	}
	if len(records) != 1 || records[0].Name != name {
		return nil, fmt.Errorf("read Slurm reservation %s: expected one matching record", name)
	}
	return &records[0], nil
}

func (s *slurmReservations) create(ctx context.Context, name string, nodes []string, lifetime time.Duration) error {
	command := "scontrol create reservation ReservationName=" + ShellQuote(name) + " StartTime=now Duration=" + slurmDuration(lifetime) + " Users=root Flags=STATIC_ALLOC Nodes=" + ShellQuote(strings.Join(nodes, ","))
	out, err := s.runtime.Controller().Run(ctx, command)
	if err != nil {
		return fmt.Errorf("create Slurm reservation %s: %w; output=%s", name, err, out)
	}
	return nil
}

func reservationContention(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "requested nodes are busy") || strings.Contains(message, "requested node configuration is not available") || strings.Contains(message, "reservation overlaps")
}

func (s *slurmReservations) delete(ctx context.Context, name string) error {
	out, err := s.runtime.Controller().Run(ctx, "scontrol delete ReservationName="+ShellQuote(name))
	observed, checkErr := s.get(ctx, name)
	if errors.Is(checkErr, errReservationAbsent) || (checkErr == nil && observed == nil) {
		return nil
	}
	return fmt.Errorf("confirm removal of Slurm reservation %s: %w; output=%s", name, errors.Join(err, checkErr, errors.New("confirm reservation removal: removal is unconfirmed")), out)
}

func (s *slurmReservations) jobs(ctx context.Context, name string) ([]string, error) {
	out, err := s.read(ctx, "squeue --all -h --reservation="+ShellQuote(name)+" -o '%i'")
	if err != nil {
		return nil, fmt.Errorf("list reservation %s jobs: %w", name, err)
	}
	return strings.Fields(out), nil
}

func (s *slurmReservations) cancelJobs(ctx context.Context, name string) error {
	_, err := s.runtime.Controller().Run(ctx, "scancel --reservation="+ShellQuote(name))
	return err
}

func (s *slurmReservations) grantUser(ctx context.Context, name, user string) error {
	_, err := s.runtime.Controller().Run(ctx, "scontrol update ReservationName="+ShellQuote(name)+" Users+="+ShellQuote(user))
	if err != nil {
		return fmt.Errorf("grant reservation %s access to %s: %w", name, user, err)
	}
	observed, err := s.get(ctx, name)
	if err != nil {
		return err
	}
	if observed == nil || !slices.Contains(observed.Users, user) {
		return fmt.Errorf("confirm reservation %s access for %s", name, user)
	}
	return nil
}

func (s *slurmReservations) powerTimeout(ctx context.Context, flag string) (time.Duration, error) {
	out, err := s.read(ctx, "scontrol show config")
	if err != nil {
		return 0, err
	}
	key := "SuspendTimeout"
	if flag == "POWERING_UP" {
		key = "ResumeTimeout"
	}
	configuration, err := parseSlurmConfiguration(out)
	if err != nil {
		return 0, SlurmConfigurationError{err: fmt.Errorf("parse effective Slurm configuration: %w", err)}
	}
	value, found := configuration[key]
	if !found {
		return 0, SlurmConfigurationError{err: fmt.Errorf("read configured %s", key)}
	}
	timeout, err := ParseSlurmSeconds(value)
	if err != nil || timeout == 0 {
		return 0, SlurmConfigurationError{err: fmt.Errorf("parse configured %s: %q: %v", key, value, err)}
	}
	return timeout + time.Minute, nil
}

func slurmDuration(duration time.Duration) string {
	seconds := int64((duration + time.Second - 1) / time.Second)
	return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}

// Slurm renders a reservation duration as [days-]hours:minutes:seconds.
func reservationDurationValue(value string) (time.Duration, error) {
	var days int64
	if day, rest, ok := strings.Cut(value, "-"); ok {
		parsed, err := strconv.ParseInt(day, 10, 32)
		if err != nil || parsed < 0 {
			return 0, fmt.Errorf("parse reservation duration %q", value)
		}
		days, value = parsed, rest
	}
	var hours, minutes, seconds int64
	count, err := fmt.Sscanf(value, "%d:%d:%d", &hours, &minutes, &seconds)
	if err != nil || count != 3 || hours < 0 || minutes < 0 || minutes >= 60 || seconds < 0 || seconds >= 60 {
		return 0, fmt.Errorf("parse reservation duration %q", value)
	}
	return time.Duration(days*86400+hours*3600+minutes*60+seconds) * time.Second, nil
}
