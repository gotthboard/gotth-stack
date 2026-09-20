package controller

import (
	"context"
	"errors"
	"strings"

	"github.com/gotthboard/gotth-stack/pkg/journal"
)

// Recover reconciles exactly one replayed in-flight call. Mutations are never
// invoked again: the controller only observes and classifies their outcome.
// Interrupted read-only calls receive the journal-mandated next attempt.
func (controller *Controller) Recover(ctx context.Context) (journal.Operation, error) {
	if controller == nil || ctx == nil {
		return journal.Operation{}, ErrInvalidInput
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	operation, ok, err := controller.journal.ActiveOperation()
	if err != nil {
		return journal.Operation{}, err
	}
	if !ok || operation.PlanDigest != controller.plan.Digest || operation.ApprovalID != controller.approval.ID {
		return journal.Operation{}, ErrInvalidInput
	}
	step, running := runningStep(operation)
	if !running {
		if operation.State == journal.StateRecoveryRequired || operation.State == journal.StateFailed {
			return controller.finishRecoveredFailure(ctx, operation, ErrAdapter)
		}
		if operation.State == journal.StateRollingBack {
			return controller.rollback(ctx, operation)
		}
		return controller.continueExecution(ctx, operation, true)
	}
	binding := controller.bindings[step.ComponentID]
	if binding == nil || !step.Recovered {
		return operation, ErrRecoveryRequired
	}
	if step.Mode == journal.ModeMutation {
		operation, err = controller.reconcileMutation(ctx, operation, binding, step)
		if err != nil {
			return operation, err
		}
		if step.Phase == journal.PhaseRollback && operation.Steps[len(operation.Steps)-1].Status == journal.StepFailed {
			finished, finishErr := controller.journal.FinishOperation(operation.ID, journal.OutcomeRecoveryRequired)
			if finishErr != nil {
				return operation, finishErr
			}
			return finished, ErrRecoveryRequired
		}
		if step.Phase == journal.PhaseApply && operation.Steps[len(operation.Steps)-1].Status == journal.StepFailed {
			if step.Rollback.RecoveryOnlyReason != "" {
				finished, finishErr := controller.journal.FinishOperation(operation.ID, journal.OutcomeRecoveryRequired)
				if finishErr != nil {
					return operation, finishErr
				}
				return finished, ErrRecoveryRequired
			}
			return controller.rollback(ctx, operation)
		}
	} else {
		operation, err = controller.retryReadOnly(ctx, operation, binding, step)
		if err != nil {
			return controller.finishRecoveredFailure(ctx, operation, err)
		}
	}
	if operation.State == journal.StateRollingBack {
		return controller.rollback(ctx, operation)
	}
	return controller.continueExecution(ctx, operation, true)
}

func (controller *Controller) finishRecoveredFailure(ctx context.Context, operation journal.Operation, cause error) (journal.Operation, error) {
	if errors.Is(cause, ErrRecoveryRequired) || errors.Is(cause, ErrObservation) {
		return operation, cause
	}
	if operation.State == journal.StateRecoveryRequired || operation.MutationStarted && hasRecoveryOnlyMutation(operation) {
		finished, err := controller.journal.FinishOperation(operation.ID, journal.OutcomeRecoveryRequired)
		if err != nil {
			return operation, err
		}
		return finished, ErrRecoveryRequired
	}
	if operation.State == journal.StateFailed && operation.MutationStarted {
		return controller.rollback(ctx, operation)
	}
	if operation.State == journal.StateFailed {
		finished, err := controller.journal.FinishOperation(operation.ID, journal.OutcomeFailed)
		if err != nil {
			return operation, err
		}
		return finished, cause
	}
	return operation, cause
}

func (controller *Controller) retryReadOnly(ctx context.Context, operation journal.Operation, binding *Binding, step journal.Step) (journal.Operation, error) {
	attempt := step.Attempt + 1
	switch {
	case strings.HasPrefix(step.StepID, "preflight-") || strings.HasPrefix(step.StepID, "refresh-"):
		return controller.runPreflight(ctx, operation, binding, step.StepID, attempt)
	case strings.HasPrefix(step.StepID, "stage-"):
		return controller.runStep(ctx, operation, binding, stepSpec{id: step.StepID, attempt: attempt, phase: journal.PhasePreflight, mode: journal.ModeReadOnly, call: binding.reconcileStage, reached: binding.stageReached})
	case strings.HasPrefix(step.StepID, "rollback-verify-"):
		return controller.runStep(ctx, operation, binding, stepSpec{id: step.StepID, attempt: attempt, phase: journal.PhaseRollback, mode: journal.ModeReadOnly, call: binding.verifyPrevious.call, reached: binding.verifyPrevious.reached})
	case strings.HasPrefix(step.StepID, "verify-"):
		return controller.runStep(ctx, operation, binding, stepSpec{id: step.StepID, attempt: attempt, phase: journal.PhaseVerify, mode: journal.ModeReadOnly, call: binding.verifyCandidate.call, reached: binding.verifyCandidate.reached})
	default:
		return operation, ErrRecoveryRequired
	}
}

func (controller *Controller) reconcileMutation(ctx context.Context, operation journal.Operation, binding *Binding, step journal.Step) (journal.Operation, error) {
	action, ok := actionForStep(binding, step.StepID)
	if !ok {
		return operation, ErrRecoveryRequired
	}
	reached, predecessor := action.reached, action.predecessor
	if step.Phase == journal.PhaseRollback {
		reached, predecessor = action.reversed, action.reached
	}
	observed, err := binding.observe(ctx, operation.ID)
	if err != nil {
		return operation, ErrObservation
	}
	result := journal.StepResultInput{OperationID: operation.ID, StepID: step.StepID, Attempt: step.Attempt}
	if reached(observed) {
		result.Status = journal.StepSucceeded
		result.ResultDigest = digest(observed)
	} else if predecessor(observed) {
		result.Status = journal.StepFailed
		result.ReasonCode = "reconciled_not_applied"
	} else {
		return operation, ErrRecoveryRequired
	}
	return controller.journal.FinishStep(result)
}

func actionForStep(binding *Binding, stepID string) (transition, bool) {
	for _, action := range binding.forward {
		if strings.HasSuffix(stepID, "-"+action.name) {
			return action, true
		}
	}
	return transition{}, false
}

func runningStep(operation journal.Operation) (journal.Step, bool) {
	for index := len(operation.Steps) - 1; index >= 0; index-- {
		if operation.Steps[index].Status == journal.StepRunning {
			return operation.Steps[index], true
		}
	}
	return journal.Step{}, false
}
