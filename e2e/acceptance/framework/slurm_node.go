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
)

type SlurmNodeInfo struct {
	Name          string
	State         string
	Reason        string
	InstanceID    string
	RealMemoryMiB uint64
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
	return info
}

func (s *SlurmClient) NodeInfo(ctx context.Context, worker string) (SlurmNodeInfo, error) {
	out, err := s.exec.Controller().RunWithDefaultRetry(ctx,
		fmt.Sprintf("scontrol show node %s", ShellQuote(worker)))
	if err != nil {
		return SlurmNodeInfo{}, fmt.Errorf("show Slurm node %s: %w", worker, err)
	}
	return ParseSlurmNodeInfo(worker, out), nil
}

func (s *SlurmClient) NodeInfoOnce(ctx context.Context, worker string) (SlurmNodeInfo, error) {
	out, err := s.exec.Controller().Run(ctx,
		fmt.Sprintf("scontrol show node %s", ShellQuote(worker)))
	if err != nil {
		return SlurmNodeInfo{}, fmt.Errorf("show Slurm node %s: %w", worker, err)
	}
	return ParseSlurmNodeInfo(worker, out), nil
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

func (n SlurmNodeInfo) IsUsable() bool {
	for _, flag := range []string{"DRAIN", "DOWN", "FAIL", "NOT_RESPONDING", "INVALID_REG"} {
		if n.HasStateFlag(flag) {
			return false
		}
	}
	return true
}

func (s *SlurmClient) WaitForNodeReasonContains(ctx context.Context, worker, reasonPart string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("Slurm node %s reason contains %q", worker, reasonPart), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := s.NodeInfo(waitCtx, worker)
		if err != nil {
			return false, err
		}
		return node.HasStateFlag("DRAIN") && node.ReasonContains(reasonPart), nil
	})
}

func (s *SlurmClient) WaitForNodeReasonCleared(ctx context.Context, worker, reasonPart string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("Slurm node %s reason does not contain %q", worker, reasonPart), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := s.NodeInfo(waitCtx, worker)
		if err != nil {
			return false, err
		}
		return !node.ReasonContains(reasonPart), nil
	})
}

func (s *SlurmClient) WaitForNodeUsable(ctx context.Context, worker string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("Slurm node %s usable", worker), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
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
		s.exec.Logf("cleanup: not resuming Slurm node %s; state=%s reason=%s", worker, node.State, node.Reason)
		return nil
	}
	if !node.ReasonContains(reasonPart) {
		s.exec.Logf("cleanup: not resuming Slurm node %s drained for different reason; state=%s reason=%s expected_reason_part=%s",
			worker, node.State, node.Reason, reasonPart)
		return nil
	}
	_, err = s.exec.Controller().RunWithDefaultRetry(ctx,
		fmt.Sprintf("scontrol update nodename=%s state=resume", ShellQuote(worker)))
	if err != nil {
		return fmt.Errorf("resume Slurm node %s after %s: %w", worker, reasonPart, err)
	}
	return nil
}
