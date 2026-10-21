package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExternalLoginServiceSkipsHeadlessService(t *testing.T) {
	headless := loginServiceForTest("soperator-login-headless-svc", corev1.ServiceTypeClusterIP)
	external := loginServiceForTest("soperator-login-svc", corev1.ServiceTypeLoadBalancer)

	service, found := externalLoginService([]corev1.Service{headless, external})
	require.True(t, found)
	assert.Equal(t, "soperator-login-svc", service.Name)

	_, found = externalLoginService([]corev1.Service{headless})
	assert.False(t, found)
}

func TestLoadBalancerSSHEndpoint(t *testing.T) {
	service := loginServiceForTest("soperator-login-svc", corev1.ServiceTypeLoadBalancer)

	_, err := loadBalancerSSHEndpoint(service)
	require.Error(t, err)
	assert.ErrorContains(t, err, "none assigned")

	service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "204.12.161.42"}}
	endpoint, err := loadBalancerSSHEndpoint(service)
	require.NoError(t, err)
	assert.Equal(t, LoginSSHEndpoint{Host: "204.12.161.42", Port: 22}, endpoint)

	service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{Hostname: "login.example.com"}}
	endpoint, err = loadBalancerSSHEndpoint(service)
	require.NoError(t, err)
	assert.Equal(t, LoginSSHEndpoint{Host: "login.example.com", Port: 22}, endpoint)
}

func TestNodePortSSHEndpoint(t *testing.T) {
	service := loginServiceForTest("soperator-login-svc", corev1.ServiceTypeNodePort)
	service.Spec.Ports[0].NodePort = 30022
	internalOnly := corev1.Node{Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
	}}}
	external := corev1.Node{Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
		{Type: corev1.NodeExternalIP, Address: "204.12.161.43"},
	}}}

	_, err := nodePortSSHEndpoint(service, []corev1.Node{internalOnly})
	require.Error(t, err)
	assert.ErrorContains(t, err, "external IP")

	endpoint, err := nodePortSSHEndpoint(service, []corev1.Node{internalOnly, external})
	require.NoError(t, err)
	assert.Equal(t, LoginSSHEndpoint{Host: "204.12.161.43", Port: 30022}, endpoint)
}

func loginServiceForTest(name string, serviceType corev1.ServiceType) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.ServiceSpec{
			Type:  serviceType,
			Ports: []corev1.ServicePort{{Port: 22}},
		},
	}
}
