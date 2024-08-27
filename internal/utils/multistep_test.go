package utils_test

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/utils"
)

func TestExecuteMultiStep(t *testing.T) {
	ctx := context.Background()
	log.IntoContext(ctx, logr.New(log.NullLogSink{}))

	t.Run("Test ExecuteMultiStep with invalid strategy", func(t *testing.T) {
		err := utils.ExecuteMultiStep(ctx, "InvalidStrategy", "Strategy")
		assert.NotNil(t, err)
		assert.ErrorContains(t, err, "invalid execution strategy \"Strategy\"")
	})

	steps := []utils.MultiStepExecutionStep{
		{
			Name: "1",
			Func: func(_ context.Context) error {
				return nil
			},
		},
		{
			Name: "2",
			Func: func(_ context.Context) error {
				return errors.New("aaa")
			},
		},
		{
			Name: "3",
			Func: func(_ context.Context) error {
				return nil
			},
		},
		{
			Name: "4",
			Func: func(_ context.Context) error {
				return errors.New("bbb")
			},
		},
		{
			Name: "5",
			Func: func(_ context.Context) error {
				return errors.New("ccc")
			},
		},
		{
			Name: "6",
			Func: func(_ context.Context) error {
				return nil
			},
		},
	}

	t.Run("Test ExecuteMultiStep with FailAtFirstError strategy", func(t *testing.T) {
		err := utils.ExecuteMultiStep(ctx, "FailAtFirstError", utils.MultiStepExecutionStrategyFailAtFirstError, steps...)
		assert.NotNil(t, err)
		assert.ErrorContains(t, err, "aaa")
	})

	t.Run("Test ExecuteMultiStep with CollectErrors strategy", func(t *testing.T) {
		err := utils.ExecuteMultiStep(ctx, "CollectErrors", utils.MultiStepExecutionStrategyCollectErrors, steps...)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "aaa")
		assert.Contains(t, err.Error(), "bbb")
		assert.Contains(t, err.Error(), "ccc")
	})

	t.Run("Test ExecuteMultiStep with no failing steps", func(t *testing.T) {
		err := utils.ExecuteMultiStep(ctx, "No failing steps", utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "Empty",
				Func: func(_ context.Context) error {
					return nil
				},
			},
		)
		assert.Nil(t, err)
	})
}
