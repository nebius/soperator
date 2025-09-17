package login

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderService_LoadBalancerSourceRanges(t *testing.T) {
	tests := []struct {
		name                               string
		loadBalancerSourceRanges           []string
		serviceType                        corev1.ServiceType
		expectedLoadBalancerSourceRanges   []string
		shouldHaveLoadBalancerSourceRanges bool
	}{
		{
			name:                               "LoadBalancer with source ranges",
			loadBalancerSourceRanges:           []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			serviceType:                        corev1.ServiceTypeLoadBalancer,
			expectedLoadBalancerSourceRanges:   []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			shouldHaveLoadBalancerSourceRanges: true,
		},
		{
			name:                               "LoadBalancer with single source range",
			loadBalancerSourceRanges:           []string{"192.168.1.0/24"},
			serviceType:                        corev1.ServiceTypeLoadBalancer,
			expectedLoadBalancerSourceRanges:   []string{"192.168.1.0/24"},
			shouldHaveLoadBalancerSourceRanges: true,
		},
		{
			name:                               "LoadBalancer without source ranges",
			loadBalancerSourceRanges:           []string{},
			serviceType:                        corev1.ServiceTypeLoadBalancer,
			expectedLoadBalancerSourceRanges:   nil,
			shouldHaveLoadBalancerSourceRanges: false,
		},
		{
			name:                               "LoadBalancer with nil source ranges",
			loadBalancerSourceRanges:           nil,
			serviceType:                        corev1.ServiceTypeLoadBalancer,
			expectedLoadBalancerSourceRanges:   nil,
			shouldHaveLoadBalancerSourceRanges: false,
		},
		{
			name:                               "NodePort with source ranges (should be ignored)",
			loadBalancerSourceRanges:           []string{"10.0.0.0/8", "172.16.0.0/12"},
			serviceType:                        corev1.ServiceTypeNodePort,
			expectedLoadBalancerSourceRanges:   nil,
			shouldHaveLoadBalancerSourceRanges: false,
		},
		{
			name:                               "ClusterIP with source ranges (should be ignored)",
			loadBalancerSourceRanges:           []string{"10.0.0.0/8"},
			serviceType:                        corev1.ServiceTypeClusterIP,
			expectedLoadBalancerSourceRanges:   nil,
			shouldHaveLoadBalancerSourceRanges: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test data
			namespace := "test-namespace"
			clusterName := "test-cluster"

			// Complete login configuration
			login := &values.SlurmLogin{
				SlurmNode: slurmv1.SlurmNode{
					K8sNodeFilterName: "test-filter",
				},
				ContainerSshd: values.Container{
					Name: "sshd",
					NodeContainer: slurmv1.NodeContainer{
						Image:           "test-sshd-image",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Port:            22,
						Resources: corev1.ResourceList{
							corev1.ResourceMemory:           resource.MustParse("1Gi"),
							corev1.ResourceCPU:              resource.MustParse("100m"),
							corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
						},
					},
				},
				Service: values.Service{
					Name:                     "test-login-service",
					Type:                     tt.serviceType,
					Protocol:                 corev1.ProtocolTCP,
					LoadBalancerSourceRanges: tt.loadBalancerSourceRanges,
					NodePort:                 30022,
				},
			}

			// Render Service
			result := RenderService(namespace, clusterName, login)

			// Check LoadBalancerSourceRanges
			if tt.shouldHaveLoadBalancerSourceRanges {
				if len(result.Spec.LoadBalancerSourceRanges) != len(tt.expectedLoadBalancerSourceRanges) {
					t.Errorf("LoadBalancerSourceRanges length = %d, want %d", len(result.Spec.LoadBalancerSourceRanges), len(tt.expectedLoadBalancerSourceRanges))
				}
				for i, expected := range tt.expectedLoadBalancerSourceRanges {
					if i >= len(result.Spec.LoadBalancerSourceRanges) || result.Spec.LoadBalancerSourceRanges[i] != expected {
						t.Errorf("LoadBalancerSourceRanges[%d] = %v, want %v", i, result.Spec.LoadBalancerSourceRanges[i], expected)
					}
				}
			} else {
				if len(result.Spec.LoadBalancerSourceRanges) > 0 {
					t.Errorf("LoadBalancerSourceRanges should be empty but got %v", result.Spec.LoadBalancerSourceRanges)
				}
			}

			// Check service type
			if result.Spec.Type != tt.serviceType {
				t.Errorf("Service type = %v, want %v", result.Spec.Type, tt.serviceType)
			}
		})
	}
}

func TestRenderService_LoadBalancerIP(t *testing.T) {
	tests := []struct {
		name                     string
		loadBalancerIP           string
		serviceType              corev1.ServiceType
		expectedLoadBalancerIP   string
		shouldHaveLoadBalancerIP bool
	}{
		{
			name:                     "LoadBalancer with IP",
			loadBalancerIP:           "192.168.1.100",
			serviceType:              corev1.ServiceTypeLoadBalancer,
			expectedLoadBalancerIP:   "192.168.1.100",
			shouldHaveLoadBalancerIP: true,
		},
		{
			name:                     "LoadBalancer without IP",
			loadBalancerIP:           "",
			serviceType:              corev1.ServiceTypeLoadBalancer,
			expectedLoadBalancerIP:   "",
			shouldHaveLoadBalancerIP: false,
		},
		{
			name:                     "NodePort with IP (should be ignored)",
			loadBalancerIP:           "192.168.1.100",
			serviceType:              corev1.ServiceTypeNodePort,
			expectedLoadBalancerIP:   "",
			shouldHaveLoadBalancerIP: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test data
			namespace := "test-namespace"
			clusterName := "test-cluster"

			// Complete login configuration
			login := &values.SlurmLogin{
				SlurmNode: slurmv1.SlurmNode{
					K8sNodeFilterName: "test-filter",
				},
				ContainerSshd: values.Container{
					Name: "sshd",
					NodeContainer: slurmv1.NodeContainer{
						Image:           "test-sshd-image",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Port:            22,
						Resources: corev1.ResourceList{
							corev1.ResourceMemory:           resource.MustParse("1Gi"),
							corev1.ResourceCPU:              resource.MustParse("100m"),
							corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
						},
					},
				},
				Service: values.Service{
					Name:           "test-login-service",
					Type:           tt.serviceType,
					Protocol:       corev1.ProtocolTCP,
					LoadBalancerIP: tt.loadBalancerIP,
					NodePort:       30022,
				},
			}

			// Render Service
			result := RenderService(namespace, clusterName, login)

			// Check LoadBalancerIP
			if tt.shouldHaveLoadBalancerIP {
				if result.Spec.LoadBalancerIP != tt.expectedLoadBalancerIP {
					t.Errorf("LoadBalancerIP = %v, want %v", result.Spec.LoadBalancerIP, tt.expectedLoadBalancerIP)
				}
			} else {
				if result.Spec.LoadBalancerIP != "" {
					t.Errorf("LoadBalancerIP should be empty but got %v", result.Spec.LoadBalancerIP)
				}
			}

			// Check service type
			if result.Spec.Type != tt.serviceType {
				t.Errorf("Service type = %v, want %v", result.Spec.Type, tt.serviceType)
			}
		})
	}
}
