package framework

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// LoginSSHEndpoint is the address customers use to reach login SSH from
// outside Kubernetes.
type LoginSSHEndpoint struct {
	Host string
	Port int32
}

// LoginSSHEndpoint returns the external SSH endpoint of the SlurmCluster login
// Service. found is false when no LoadBalancer or NodePort login Service exists.
// An exposed Service without an assigned address yet is reported as an error,
// so callers can poll until the address appears.
func (c *KubectlClient) LoginSSHEndpoint(ctx context.Context, clusterName string) (LoginSSHEndpoint, bool, error) {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		return LoginSSHEndpoint{}, false, fmt.Errorf("SlurmCluster name is empty")
	}

	selector := fmt.Sprintf("app.kubernetes.io/instance=%s,app.kubernetes.io/component=login", clusterName)
	var services corev1.ServiceList
	if err := c.GetJSON(ctx, &services,
		"get", "services", "-n", SoperatorNamespace, "-l", selector, "-o", "json"); err != nil {
		return LoginSSHEndpoint{}, false, fmt.Errorf("list login Services for SlurmCluster %s: %w", clusterName, err)
	}
	service, found := externalLoginService(services.Items)
	if !found {
		return LoginSSHEndpoint{}, false, nil
	}

	var endpoint LoginSSHEndpoint
	var err error
	switch service.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		endpoint, err = loadBalancerSSHEndpoint(service)
	case corev1.ServiceTypeNodePort:
		var nodes corev1.NodeList
		if err := c.GetJSON(ctx, &nodes, "get", "nodes", "-o", "json"); err != nil {
			return LoginSSHEndpoint{}, true, fmt.Errorf("list nodes for NodePort Service %s: %w", service.Name, err)
		}
		endpoint, err = nodePortSSHEndpoint(service, nodes.Items)
	}
	if err != nil {
		return LoginSSHEndpoint{}, true, err
	}
	return endpoint, true, nil
}

func externalLoginService(services []corev1.Service) (corev1.Service, bool) {
	for _, service := range services {
		switch service.Spec.Type {
		case corev1.ServiceTypeLoadBalancer, corev1.ServiceTypeNodePort:
			return service, true
		}
	}
	return corev1.Service{}, false
}

func loadBalancerSSHEndpoint(service corev1.Service) (LoginSSHEndpoint, error) {
	if len(service.Spec.Ports) == 0 {
		return LoginSSHEndpoint{}, fmt.Errorf("find port of LoadBalancer Service %s", service.Name)
	}
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		host := ingress.IP
		if host == "" {
			host = ingress.Hostname
		}
		if host != "" {
			return LoginSSHEndpoint{Host: host, Port: service.Spec.Ports[0].Port}, nil
		}
	}
	return LoginSSHEndpoint{}, fmt.Errorf("find ingress address of LoadBalancer Service %s: none assigned", service.Name)
}

func nodePortSSHEndpoint(service corev1.Service, nodes []corev1.Node) (LoginSSHEndpoint, error) {
	if len(service.Spec.Ports) == 0 || service.Spec.Ports[0].NodePort == 0 {
		return LoginSSHEndpoint{}, fmt.Errorf("find node port of NodePort Service %s", service.Name)
	}
	for _, node := range nodes {
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeExternalIP && address.Address != "" {
				return LoginSSHEndpoint{Host: address.Address, Port: service.Spec.Ports[0].NodePort}, nil
			}
		}
	}
	return LoginSSHEndpoint{}, fmt.Errorf("find node with an external IP for NodePort Service %s", service.Name)
}
