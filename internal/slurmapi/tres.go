package slurmapi

import (
	"fmt"
	"strconv"
	"strings"
)

type TrackableResources struct {
	CPUCount    int
	MemoryBytes int
	GPUCount    int
}

// ParseTrackableResources parses a string like "cpu=16,mem=191356M,billing=16,gres/gpu=1"
func ParseTrackableResources(input string) (*TrackableResources, error) {
	spec := &TrackableResources{}

	pairs := strings.Split(input, ",")

	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "cpu":
			cpu, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid cpu value: %s", value)
			}
			spec.CPUCount = cpu

		case "mem":
			memBytes, err := parseMemoryValue(value)
			if err != nil {
				return nil, err
			}
			spec.MemoryBytes = memBytes

		case "gres/gpu":
			gpu, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid gpu value: %s", value)
			}
			spec.GPUCount = gpu
		}
	}

	return spec, nil
}

func parseMemoryValue(value string) (int, error) {
	if len(value) == 0 {
		return 0, fmt.Errorf("empty memory value")
	}

	lastChar := value[len(value)-1:]
	multiplier := int64(1)
	var numStr string

	if lastChar >= "0" && lastChar <= "9" {
		numStr = value
	} else {
		switch strings.ToLower(lastChar) {
		case "k":
			multiplier = 1024
		case "m":
			multiplier = 1024 * 1024
		case "g":
			multiplier = 1024 * 1024 * 1024
		case "t":
			multiplier = 1024 * 1024 * 1024 * 1024
		case "p":
			multiplier = 1024 * 1024 * 1024 * 1024 * 1024
		default:
			return 0, fmt.Errorf("unknown memory suffix: %s", lastChar)
		}
		numStr = value[:len(value)-1]
	}

	num, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory value: %s", value)
	}
	return int(num * multiplier), nil
}
