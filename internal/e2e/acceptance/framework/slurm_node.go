package framework

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	slurmNodeStatePattern      = regexp.MustCompile(`\bState=([^\s]+)`)
	slurmNodeReasonPattern     = regexp.MustCompile(`\bReason=([^\n]+)`)
	slurmNodeInstanceIDPattern = regexp.MustCompile(`\bInstanceId=([^\s]+)`)
)

type SlurmNodeInfo struct {
	Name       string
	Raw        string
	State      string
	Reason     string
	InstanceID string
}

func ParseSlurmNodeInfo(name, output string) SlurmNodeInfo {
	info := SlurmNodeInfo{
		Name: name,
		Raw:  output,
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

func (n SlurmNodeInfo) HasStateFlag(flag string) bool {
	for _, part := range strings.Split(n.State, "+") {
		if strings.EqualFold(strings.TrimSpace(part), flag) {
			return true
		}
	}
	return false
}

func (n SlurmNodeInfo) ReasonContains(reasonPart string) bool {
	return strings.Contains(n.Raw, reasonPart) || strings.Contains(n.Reason, reasonPart)
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

func (s *SlurmClient) WaitForNodeUsableWithoutReason(ctx context.Context, worker, reasonPart string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("Slurm node %s usable without %q", worker, reasonPart), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := s.NodeInfo(waitCtx, worker)
		if err != nil {
			return false, err
		}
		if node.ReasonContains(reasonPart) {
			return false, nil
		}
		return node.IsUsable(), nil
	})
}

func (s *SlurmClient) ResumeNodeIfDrainedByReason(ctx context.Context, worker, reasonPart string) error {
	node, err := s.NodeInfo(ctx, worker)
	if err != nil {
		return err
	}
	if !node.HasStateFlag("DRAIN") || !node.ReasonContains(reasonPart) {
		return nil
	}
	_, err = s.exec.Controller().RunWithDefaultRetry(ctx,
		fmt.Sprintf("scontrol update nodename=%s state=resume", ShellQuote(worker)))
	if err != nil {
		return fmt.Errorf("resume Slurm node %s after %s: %w", worker, reasonPart, err)
	}
	return nil
}
