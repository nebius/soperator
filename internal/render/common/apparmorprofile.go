package common

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// ParseAppArmorProfile converts AppArmor profile string to corev1.AppArmorProfile
// It supports formats like "unconfined", "localhost/profile-name", or just "profile-name"
func ParseAppArmorProfile(profileStr string) *corev1.AppArmorProfile {
	if profileStr == "" {
		return nil
	}
	if profileStr == "unconfined" {
		return &corev1.AppArmorProfile{
			Type: corev1.AppArmorProfileTypeUnconfined,
		}
	}
	if strings.HasPrefix(profileStr, "localhost/") {
		profileName := strings.TrimPrefix(profileStr, "localhost/")
		return &corev1.AppArmorProfile{
			Type:             corev1.AppArmorProfileTypeLocalhost,
			LocalhostProfile: ptr.To(profileName),
		}
	}
	return &corev1.AppArmorProfile{
		Type:             corev1.AppArmorProfileTypeLocalhost,
		LocalhostProfile: ptr.To(profileStr),
	}
}
