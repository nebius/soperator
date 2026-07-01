package e2e

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// terraform init pulls providers from the public registry (registry.terraform.io,
// proxied to github.com) and from the Nebius private registry. Both intermittently
// return 5xx, reset, or refused connections in CI, and terraform's own two internal
// attempts per provider are often not enough to ride out a blip. Wrap the whole init
// in an outer retry with exponential backoff.
//
// init is idempotent, so we retry on any error rather than trying to enumerate the
// broad and shifting set of transient sub-errors. A genuinely broken config only
// costs the bounded backoff before the real error is surfaced.
const (
	initMaxAttempts = 4
	initBackoffBase = 10 * time.Second
)

// initWithRetry runs tf.Init with the retry policy above. It is the single init
// entry point for the apply, destroy, and cleanup paths.
func initWithRetry(ctx context.Context, tf *tfexec.Terraform, opts ...tfexec.InitOption) error {
	return retryInit(ctx, initMaxAttempts, initBackoffBase, func(c context.Context) error {
		return tf.Init(c, opts...)
	})
}

// retryInit calls run until it succeeds or attempts are exhausted, waiting base,
// then 2*base, 4*base, ... between tries. It aborts early if ctx is cancelled.
func retryInit(ctx context.Context, attempts int, base time.Duration, run func(context.Context) error) error {
	if attempts < 1 {
		attempts = 1
	}

	backoff := base
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = run(ctx); err == nil {
			return nil
		}
		if attempt == attempts {
			break
		}

		log.Printf("Terraform init attempt %d/%d failed, retrying in %s: %v", attempt, attempts, backoff, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}

	return fmt.Errorf("after %d attempts: %w", attempts, err)
}
