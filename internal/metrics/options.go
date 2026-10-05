package metrics

import (
	"crypto/tls"

	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func ServerOptions(bindAddress string, secure bool, tlsOpts []func(*tls.Config)) metricsserver.Options {
	opts := metricsserver.Options{
		BindAddress:   bindAddress,
		SecureServing: secure,
		TLSOpts:       tlsOpts,
	}
	if secure {
		opts.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
	return opts
}
