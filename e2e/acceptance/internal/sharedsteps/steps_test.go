package sharedsteps

import (
	"context"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type cleanupTestFamily struct {
	cleanup func(context.Context)
}

func (*cleanupTestFamily) RegisterSteps(*godog.ScenarioContext)  {}
func (f *cleanupTestFamily) CleanupAndReset(ctx context.Context) { f.cleanup(ctx) }

func TestCleanupStepFamilyPreservesOwnershipAfterCancellation(t *testing.T) {
	ctx := framework.WithWorkerReservation(t.Context(), framework.NewReservationManager(), "cleanup")
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	family := &cleanupTestFamily{cleanup: func(ctx context.Context) {
		assert.NoError(t, ctx.Err())
		framework.RecordWorkerCleanupError(ctx, assert.AnError)
	}}
	require.NoError(t, cleanupStepFamily(ctx, family))
	require.ErrorIs(t, framework.FinishWorkerReservation(ctx, nil), assert.AnError)
}

func TestCleanupStepFamilyReturnsPanicForFinalization(t *testing.T) {
	ctx := framework.WithWorkerReservation(t.Context(), framework.NewReservationManager(), "panic")
	family := &cleanupTestFamily{cleanup: func(context.Context) { panic("restore interrupted") }}
	err := cleanupStepFamily(ctx, family)
	require.ErrorContains(t, err, "restore interrupted")
	framework.RecordWorkerCleanupError(ctx, err)
	require.EqualError(t, framework.FinishWorkerReservation(ctx, nil), err.Error())
}
