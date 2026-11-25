package sharedsteps

import (
	"fmt"
	"slices"
	"strings"
)

const (
	nvidiaDriverCapabilitiesName = "NVIDIA_DRIVER_CAPABILITIES"
	nvidiaVisibleDevicesName     = "NVIDIA_VISIBLE_DEVICES"
	nvidiaDriverCapabilityAll    = "all"
)

// parseNVIDIADriverCapabilities splits a NVIDIA_DRIVER_CAPABILITIES value the
// way the worker entrypoint does: comma separated, whitespace around names
// ignored, "all" standing for every capability and kept as is.
func parseNVIDIADriverCapabilities(value string) ([]string, error) {
	var capabilities []string
	for _, capability := range strings.Split(value, ",") {
		capability = strings.TrimSpace(capability)
		if capability == nvidiaDriverCapabilityAll {
			return []string{nvidiaDriverCapabilityAll}, nil
		}
		if capability != "" && !slices.Contains(capabilities, capability) {
			capabilities = append(capabilities, capability)
		}
	}
	if len(capabilities) == 0 {
		return nil, fmt.Errorf("parse %s=%q: no capabilities", nvidiaDriverCapabilitiesName, value)
	}
	return capabilities, nil
}

// nvidiaCapabilityRequested reports whether the list, possibly the "all"
// alias, includes the capability.
func nvidiaCapabilityRequested(capabilities []string, capability string) bool {
	return slices.Contains(capabilities, nvidiaDriverCapabilityAll) || slices.Contains(capabilities, capability)
}

// missingNVIDIADriverCapabilities lists the required capabilities absent from
// the configured ones, in required order.
func missingNVIDIADriverCapabilities(configured, required []string) []string {
	var missing []string
	for _, capability := range required {
		if !nvidiaCapabilityRequested(configured, capability) {
			missing = append(missing, capability)
		}
	}
	return missing
}
