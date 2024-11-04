package common

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apparmorprofileapi "sigs.k8s.io/security-profiles-operator/api/apparmorprofile/v1alpha1"
)

func RenderAppArmorProfile(clusterName, namespace, profileName, appArmorPolicyTemplate string) *apparmorprofileapi.AppArmorProfile {
	return &apparmorprofileapi.AppArmorProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      profileName,
			Namespace: namespace,
		},
		Spec: apparmorprofileapi.AppArmorProfileSpec{
			Policy: fmt.Sprintf(appArmorPolicyTemplate, profileName),
		},
	}
}
