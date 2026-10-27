package worker

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
)

// nodeStaticMetadataFields are the nodeConfig.static fields exported as SOPERATOR_NODE_<FIELD> variables.
// Other fields are ignored so that unrelated changes don't restart workers.
var nodeStaticMetadataFields = []string{"Gres", "CPUs", "ThreadsPerCore", "CoresPerSocket", "SocketsPerBoard"}

var (
	gresCountPattern  = regexp.MustCompile(`(?i)^([0-9]+)([KMGTP]?)$`)
	gresCountSuffixes = map[string]uint{"": 0, "K": 10, "M": 20, "G": 30, "T": 40, "P": 50}
	gpuTypeSeparator  = regexp.MustCompile(`[^A-Z0-9]+`)
)

const gpuTypeVendor = "NVIDIA"

func renderNodeMetadataEnv(static string, gpuEnabled bool) ([]corev1.EnvVar, error) {
	fields := parseNodeStatic(static)
	var env []corev1.EnvVar
	for _, field := range nodeStaticMetadataFields {
		if value, ok := fields[field]; ok {
			env = append(env, corev1.EnvVar{Name: consts.EnvNodePrefix + strings.ToUpper(field), Value: value})
		}
	}

	tags := []string{"CPU"}
	if gpuEnabled {
		var err error
		if tags, err = gpuPlatformTags(fields["Gres"]); err != nil {
			return nil, err
		}
	}
	env = append(env,
		corev1.EnvVar{Name: consts.EnvNodePlatformTag, Value: tags[0]},
		corev1.EnvVar{Name: consts.EnvNodePlatformTags, Value: strings.Join(tags, ",")},
	)
	return env, nil
}

// parseNodeStatic extracts nodeStaticMetadataFields from nodeConfig.static, matching keys case-insensitively.
func parseNodeStatic(static string) map[string]string {
	fields := make(map[string]string)
	for _, token := range strings.Fields(static) {
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			continue
		}
		for _, field := range nodeStaticMetadataFields {
			if strings.EqualFold(key, field) {
				fields[field] = strings.Trim(value, `"`)
			}
		}
	}
	return fields
}

// gpuPlatformTags returns platform tags of a GPU worker, the most specific first:
// "<count>x<model>" when all GPUs have the same model in their Gres type, and always "<count>xGPU".
// Gres format: https://slurm.schedmd.com/slurm.conf.html#OPT_Gres_1
func gpuPlatformTags(gres string) ([]string, error) {
	var total uint64
	var model string
	found := false
	for _, spec := range splitGres(gres) {
		name, _, _ := strings.Cut(spec, ":")
		name, _, _ = strings.Cut(name, "(")
		if !strings.EqualFold(strings.TrimSpace(name), "gpu") {
			continue
		}
		gresType, count, err := parseGPUGres(spec)
		if err != nil {
			return nil, fmt.Errorf("parse nodeConfig.static Gres %q: %w", spec, err)
		}
		if count > math.MaxUint64-total {
			return nil, fmt.Errorf("parse nodeConfig.static Gres %q: GPU count exceeds uint64", gres)
		}
		total += count
		specModel := gpuModel(gresType)
		if !found {
			model = specModel
		} else if model != specModel {
			model = ""
		}
		found = true
	}
	if total == 0 {
		return nil, fmt.Errorf("provide Gres=gpu[:<type>]:<count> in nodeConfig.static for a GPU worker")
	}

	var tags []string
	if model != "" {
		tags = append(tags, fmt.Sprintf("%dx%s", total, model))
	}
	return append(tags, fmt.Sprintf("%dxGPU", total)), nil
}

// splitGres splits Gres by commas outside of parentheses, e.g. "gpu:h100:8(S:0,1),nic:2".
func splitGres(gres string) []string {
	var specs []string
	depth, start := 0, 0
	for i, r := range gres + "," {
		switch r {
		case '(':
			depth++
		case ')':
			depth = max(depth-1, 0)
		case ',':
			if depth > 0 {
				continue
			}
			if spec := strings.TrimSpace(gres[start:i]); spec != "" {
				specs = append(specs, spec)
			}
			start = i + 1
		}
	}
	return specs
}

// parseGPUGres parses "gpu[:<type>][:no_consume]:<count>[K|M|G|T|P][(<binding>)]".
func parseGPUGres(spec string) (string, uint64, error) {
	spec, _, _ = strings.Cut(spec, "(")
	var parts []string
	for _, part := range strings.Split(spec, ":")[1:] {
		if !strings.EqualFold(part, "no_consume") {
			parts = append(parts, strings.TrimSpace(part))
		}
	}

	var gresType string
	switch len(parts) {
	case 0:
		// Slurm counts a GRES without a count as 1.
		return "", 1, nil
	case 1:
	case 2:
		gresType = parts[0]
		if gresType == "" {
			return "", 0, fmt.Errorf("expected non-empty type")
		}
	default:
		return "", 0, fmt.Errorf("expected gpu[:<type>]:<count>")
	}

	match := gresCountPattern.FindStringSubmatch(parts[len(parts)-1])
	if match == nil {
		return "", 0, fmt.Errorf("expected count with an optional K, M, G, T or P suffix")
	}
	count, err := strconv.ParseUint(match[1], 10, 64)
	shift := gresCountSuffixes[strings.ToUpper(match[2])]
	if err != nil || count > math.MaxUint64>>shift {
		return "", 0, fmt.Errorf("count exceeds uint64")
	}
	return gresType, count << shift, nil
}

// gpuModel extracts the GPU model from a Gres type, e.g. "H100" from "nvidia_h100_80gb_hbm3".
func gpuModel(gresType string) string {
	for _, word := range gpuTypeSeparator.Split(strings.ToUpper(gresType), -1) {
		if word != "" && word != gpuTypeVendor && word != "GPU" {
			return word
		}
	}
	return ""
}
