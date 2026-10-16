package framework

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	slurmNodeStatePattern      = regexp.MustCompile(`\bState=([^\s]+)`)
	slurmNodeReasonPattern     = regexp.MustCompile(`\bReason=([^\n]+)`)
	slurmNodeInstanceIDPattern = regexp.MustCompile(`\bInstanceId=([^\s]+)`)
	slurmNodeRealMemoryPattern = regexp.MustCompile(`\bRealMemory=(\d+)`)
	slurmNodeGPUCountPattern   = regexp.MustCompile(`\bCfgTRES=[^\s]*\bgres/gpu=(\d+)`)
	slurmNodeCPUAllocPattern   = regexp.MustCompile(`\bCPUAlloc=(\S+)`)
	slurmNodeAllocMemPattern   = regexp.MustCompile(`\bAllocMem=(\S+)`)
	slurmdStartTimePattern     = regexp.MustCompile(`\bSlurmdStartTime=([^\s]+)`)
)

const slurmTimestampLayout = "2006-01-02T15:04:05"

var (
	unhealthySlurmNodeStateFlags = []string{"DRAIN", "DOWN", "FAIL", "NOT_RESPONDING", "INVALID_REG"}
	inactiveSlurmNodeStateFlags  = []string{"POWERED_DOWN", "POWERING_UP", "POWERING_DOWN", "POWER_DOWN"}
)

type SlurmNodeInfo struct {
	Name            string
	State           string
	Reason          string
	InstanceID      string
	RealMemoryMiB   uint64
	GPUCount        int
	SlurmdStartTime time.Time
	CPUAlloc        *uint64
	AllocMemoryMiB  *uint64
}

func ParseSlurmNodeInfo(name, output string) SlurmNodeInfo {
	info := SlurmNodeInfo{
		Name: name,
	}
	if match := slurmNodeStatePattern.FindStringSubmatch(output); len(match) == 2 {
		info.State = strings.TrimSpace(match[1])
	}
	if match := slurmNodeReasonPattern.FindStringSubmatch(output); len(match) == 2 {
		info.Reason = strings.TrimSpace(match[1])
	}
	if match := slurmNodeInstanceIDPattern.FindStringSubmatch(output); len(match) == 2 {
		info.InstanceID = strings.TrimSpace(match[1])
	}
	if match := slurmNodeRealMemoryPattern.FindStringSubmatch(output); len(match) == 2 {
		if value, err := strconv.ParseUint(match[1], 10, 64); err == nil {
			info.RealMemoryMiB = value
		}
	}
	if match := slurmNodeGPUCountPattern.FindStringSubmatch(output); len(match) == 2 {
		if value, err := strconv.Atoi(match[1]); err == nil {
			info.GPUCount = value
		}
	}
	if match := slurmdStartTimePattern.FindStringSubmatch(output); len(match) == 2 {
		if value, err := time.Parse(slurmTimestampLayout, match[1]); err == nil {
			info.SlurmdStartTime = value
		}
	}
	info.CPUAlloc = parseSlurmAllocation(slurmNodeCPUAllocPattern, output)
	info.AllocMemoryMiB = parseSlurmAllocation(slurmNodeAllocMemPattern, output)
	return info
}

func parseSlurmAllocation(pattern *regexp.Regexp, output string) *uint64 {
	match := pattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return nil
	}
	value, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		return nil
	}
	return &value
}

func (s *SlurmClient) NodeInfo(ctx context.Context, worker string) (SlurmNodeInfo, error) {
	out, err := s.runtime.Controller().RunWithDefaultRetry(ctx,
		fmt.Sprintf("scontrol show node %s", ShellQuote(worker)))
	if err != nil {
		return SlurmNodeInfo{}, fmt.Errorf("show Slurm node %s: %w", worker, err)
	}
	return ParseSlurmNodeInfo(worker, out), nil
}

func (s *SlurmClient) NodeInfoOnce(ctx context.Context, worker string) (SlurmNodeInfo, error) {
	out, err := s.runtime.Controller().Run(ctx,
		fmt.Sprintf("scontrol show node %s", ShellQuote(worker)))
	if err != nil {
		return SlurmNodeInfo{}, fmt.Errorf("show Slurm node %s: %w", worker, err)
	}
	return ParseSlurmNodeInfo(worker, out), nil
}

// NodeInfosOnce reads the requested nodes in one controller query without
// retries and returns them in the caller's order.
func (s *SlurmClient) NodeInfosOnce(ctx context.Context, names []string) ([]SlurmNodeInfo, error) {
	if len(names) == 0 {
		return nil, nil
	}
	requested := make(map[string]bool, len(names))
	for _, name := range names {
		if !IsSlurmNodeName(name) || requested[name] {
			return nil, fmt.Errorf("read Slurm nodes: invalid or duplicate node name %q", name)
		}
		requested[name] = true
	}
	output, err := s.runtime.Controller().Run(ctx, "scontrol -o show nodes")
	if err != nil {
		return nil, fmt.Errorf("show Slurm nodes: %w", err)
	}
	byName := make(map[string]SlurmNodeInfo, len(names))
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name, found := strings.CutPrefix(fields[0], "NodeName=")
		if !found || !IsSlurmNodeName(name) {
			return nil, fmt.Errorf("parse Slurm node record: invalid leading field %q", fields[0])
		}
		if !requested[name] {
			continue
		}
		if _, found := byName[name]; found {
			return nil, fmt.Errorf("read Slurm nodes: duplicate record for %s", name)
		}
		node := ParseSlurmNodeInfo(name, line)
		if strings.TrimSpace(node.State) == "" {
			return nil, fmt.Errorf("read Slurm node %s: missing state", name)
		}
		byName[name] = node
	}
	nodes := make([]SlurmNodeInfo, 0, len(names))
	for _, name := range names {
		node, found := byName[name]
		if !found {
			return nil, fmt.Errorf("read Slurm nodes: missing record for %s", name)
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (s *SlurmClient) MainPartitionNodeNames(ctx context.Context) ([]string, error) {
	out, err := s.runtime.Controller().RunWithDefaultRetry(ctx, `sinfo -hN -p main -o '%N'`)
	if err != nil {
		return nil, fmt.Errorf("list Slurm main partition nodes: %w", err)
	}
	return ParseSlurmNodeNames(out), nil
}

func (s *SlurmClient) ActiveWorkerStartTimes(ctx context.Context) (map[string]time.Time, error) {
	names, err := s.MainPartitionNodeNames(ctx)
	if err != nil {
		return nil, err
	}

	startTimes := make(map[string]time.Time, len(names))
	for _, name := range names {
		node, err := s.NodeInfo(ctx, name)
		if err != nil {
			return nil, err
		}
		if !node.IsReadyForImmediateWork() {
			continue
		}
		if node.SlurmdStartTime.IsZero() {
			return nil, fmt.Errorf("read valid SlurmdStartTime for active worker %s", name)
		}
		startTimes[name] = node.SlurmdStartTime
	}
	if len(startTimes) == 0 {
		return nil, fmt.Errorf("find active workers with SlurmdStartTime")
	}
	return startTimes, nil
}

func ParseSlurmNodeNames(output string) []string {
	seen := make(map[string]struct{})
	var names []string

	for _, line := range strings.Split(output, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func (n SlurmNodeInfo) HasStateFlag(flag string) bool {
	for _, part := range strings.Split(n.State, "+") {
		if strings.EqualFold(strings.TrimSpace(part), flag) {
			return true
		}
	}
	return false
}

func (n SlurmNodeInfo) ReasonContains(reasonPart string) bool {
	return strings.Contains(n.Reason, reasonPart)
}

// IsHealthy reports whether Slurm has marked the node with a failure or
// administrative state that prevents normal scheduling. A powered-down node
// can still be healthy because Slurm may power it up for a job.
func (n SlurmNodeInfo) IsHealthy() bool {
	for _, flag := range unhealthySlurmNodeStateFlags {
		if n.HasStateFlag(flag) {
			return false
		}
	}
	return true
}

// IsReadyForImmediateWork reports whether the node is healthy and has a
// running slurmd rather than being powered down or in a power transition.
func (n SlurmNodeInfo) IsReadyForImmediateWork() bool {
	if !n.IsHealthy() {
		return false
	}
	for _, flag := range inactiveSlurmNodeStateFlags {
		if n.HasStateFlag(flag) {
			return false
		}
	}
	return true
}

// IsUsable retains the worker-selection semantics: healthy powered-down nodes
// remain schedulable because Slurm can power them up on demand.
func (n SlurmNodeInfo) IsUsable() bool {
	return n.IsHealthy()
}

func (n SlurmNodeInfo) IsUsableReplacement(originalInstanceID string) bool {
	return n.InstanceID != "" && n.InstanceID != originalInstanceID && n.State != "" && n.IsUsable()
}

func (s *SlurmClient) WaitForNodeReasonContains(ctx context.Context, worker, reasonPart string, timeout time.Duration) error {
	return s.runtime.WaitFor(ctx, fmt.Sprintf("Slurm node %s reason contains %q", worker, reasonPart), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := s.NodeInfo(waitCtx, worker)
		if err != nil {
			return false, err
		}
		return node.HasStateFlag("DRAIN") && node.ReasonContains(reasonPart), nil
	})
}

func (s *SlurmClient) WaitForNodeReasonCleared(ctx context.Context, worker, reasonPart string, timeout time.Duration) error {
	return s.runtime.WaitFor(ctx, fmt.Sprintf("Slurm node %s reason does not contain %q", worker, reasonPart), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := s.NodeInfo(waitCtx, worker)
		if err != nil {
			return false, err
		}
		return !node.ReasonContains(reasonPart), nil
	})
}

func (s *SlurmClient) WaitForNodeUsable(ctx context.Context, worker string, timeout time.Duration) error {
	return s.runtime.WaitFor(ctx, fmt.Sprintf("Slurm node %s usable", worker), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := s.NodeInfo(waitCtx, worker)
		if err != nil {
			return false, err
		}
		return node.IsUsable(), nil
	})
}

func (s *SlurmClient) ResumeNodeIfDrainedByReason(ctx context.Context, worker, reasonPart string) error {
	node, err := s.NodeInfo(ctx, worker)
	if err != nil {
		return err
	}
	if !node.HasStateFlag("DRAIN") {
		s.runtime.Logf("cleanup: not resuming Slurm node %s; state=%s reason=%s", worker, node.State, node.Reason)
		return nil
	}
	if !node.ReasonContains(reasonPart) {
		s.runtime.Logf("cleanup: not resuming Slurm node %s drained for different reason; state=%s reason=%s expected_reason_part=%s",
			worker, node.State, node.Reason, reasonPart)
		return nil
	}
	_, err = s.runtime.Controller().RunWithDefaultRetry(ctx,
		fmt.Sprintf("scontrol update nodename=%s state=resume", ShellQuote(worker)))
	if err != nil {
		return fmt.Errorf("resume Slurm node %s after %s: %w", worker, reasonPart, err)
	}
	return nil
}
