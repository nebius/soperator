package metrics

import "testing"

func TestServerOptionsEnablesAuthFilterForSecureMetrics(t *testing.T) {
	opts := ServerOptions(":8443", true, nil)

	if opts.BindAddress != ":8443" {
		t.Fatalf("expected bind address :8443, got %q", opts.BindAddress)
	}
	if !opts.SecureServing {
		t.Fatal("expected secure serving to be enabled")
	}
	if opts.FilterProvider == nil {
		t.Fatal("expected authn/authz filter provider for secure metrics")
	}
}

func TestServerOptionsLeavesPlainMetricsUnfiltered(t *testing.T) {
	opts := ServerOptions(":8080", false, nil)

	if opts.BindAddress != ":8080" {
		t.Fatalf("expected bind address :8080, got %q", opts.BindAddress)
	}
	if opts.SecureServing {
		t.Fatal("expected secure serving to be disabled")
	}
	if opts.FilterProvider != nil {
		t.Fatal("expected no authn/authz filter provider for plain metrics")
	}
}
