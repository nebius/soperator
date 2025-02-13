package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type MultiStepExecutionStepFunc = func(stepCtx context.Context) error
type MultiStepExecutionStep struct {
	Name string
	Func MultiStepExecutionStepFunc
}

type MultiStepExecutionStrategy string

const (
	MultiStepExecutionStrategyFailAtFirstError MultiStepExecutionStrategy = "FailAtFirstError"
	MultiStepExecutionStrategyCollectErrors    MultiStepExecutionStrategy = "CollectErrors"
)

const (
	multiStepExecutionLogKeyID        = "multiStepExecution.Group.ID"
	multiStepExecutionLogKeyGroupName = "multiStepExecution.Group.Name"
	multiStepExecutionLogKeyStrategy  = "multiStepExecution.Group.Strategy"
	multiStepExecutionLogKeyStepID    = "multiStepExecution.Step.ID"
	multiStepExecutionLogKeyStepName  = "multiStepExecution.Step.Name"
)

func ExecuteMultiStep(ctx context.Context, name string, strategy MultiStepExecutionStrategy, steps ...MultiStepExecutionStep) error {
	var err error

	executionID := uuid.NewUUID()
	executionCtx := context.WithValue(ctx, multiStepExecutionLogKeyID, executionID)
	executionCtx = log.IntoContext(executionCtx, log.FromContext(ctx).WithValues(
		multiStepExecutionLogKeyID, executionID,
		multiStepExecutionLogKeyGroupName, name,
		multiStepExecutionLogKeyStrategy, strategy,
	))
	logger := log.FromContext(executionCtx)

	logger.V(1).Info("Executing steps")
	switch strategy {
	case MultiStepExecutionStrategyFailAtFirstError:
		err = executeFailAtFirstError(executionCtx, steps...)
	case MultiStepExecutionStrategyCollectErrors:
		err = executeCollectErrors(executionCtx, steps...)
	default:
		err = fmt.Errorf("invalid execution strategy %q", strategy)
	}

	if err != nil {
		logger.Error(err, "Failed to execute steps")
		return errors.Wrap(err, "failed to execute steps")
	} else {
		logger.V(1).Info("Executed steps")
	}

	return nil
}

func executeFailAtFirstError(ctx context.Context, steps ...MultiStepExecutionStep) error {
	for _, step := range steps {
		stepID := uuid.NewUUID()
		stepCtx := context.WithValue(ctx, multiStepExecutionLogKeyStepID, stepID)
		stepCtx = log.IntoContext(stepCtx, log.FromContext(ctx).WithValues(
			multiStepExecutionLogKeyStepID, stepID,
			multiStepExecutionLogKeyStepName, step.Name,
		))
		stepLogger := log.FromContext(stepCtx)

		stepLogger.V(1).Info("Executing step")
		err := step.Func(stepCtx)
		if err != nil {
			stepLogger.Error(err, "Failed step. Stopping execution of the following steps")
			return errors.Wrap(err, "multi-step execution failed at the first met error")
		}
	}
	return nil
}

func executeCollectErrors(ctx context.Context, steps ...MultiStepExecutionStep) error {
	var errs []string
	for _, step := range steps {
		stepID := uuid.NewUUID()
		stepCtx := context.WithValue(ctx, multiStepExecutionLogKeyStepID, stepID)
		stepCtx = log.IntoContext(stepCtx, log.FromContext(ctx).WithValues(
			multiStepExecutionLogKeyStepID, stepID,
			multiStepExecutionLogKeyStepName, step.Name,
		))
		stepLogger := log.FromContext(stepCtx)

		stepLogger.V(1).Info("Executing step")
		err := step.Func(stepCtx)
		if err != nil {
			stepLogger.Error(err, "Failed step. Executing the following steps")
			errs = append(errs, fmt.Sprintf("%s: %s", step.Name, err.Error()))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("multi-step execution failed with collected errors: %s", strings.Join(errs, "; "))
	}

	return nil
}
