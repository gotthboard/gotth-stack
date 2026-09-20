package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gotthboard/gotth-stack/pkg/journal"
)

func (controller *Controller) Execute(ctx context.Context, input ExecuteInput) (journal.Operation, error) {
	if controller == nil || ctx == nil || input.OperationID == "" {
		return journal.Operation{}, ErrInvalidInput
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	operation, err := controller.journal.StartOperation(journal.OperationInput{ID: input.OperationID, ApprovalID: controller.approval.ID, PlanDigest: controller.plan.Digest})
	if err != nil {
		return journal.Operation{}, err
	}
	if !operation.FinishedAt.IsZero() {
		switch operation.State {
		case journal.StateComplete:
			return operation, nil
		case journal.StateRecoveryRequired:
			return operation, ErrRecoveryRequired
		default:
			return operation, ErrAdapter
		}
	}
	if len(operation.Steps) != 0 {
		return operation, ErrRecoveryRequired
	}
	return controller.continueExecution(ctx, operation, false)
}

func (controller *Controller) continueExecution(ctx context.Context, operation journal.Operation, recovering bool) (journal.Operation, error) {
	var err error
	rollbackReferences := make(map[string]string, len(controller.ordered))
	for index, binding := range controller.ordered {
		stepID := fmt.Sprintf("preflight-%02d", index+1)
		if !stepSucceeded(operation, stepID) {
			operation, err = controller.runPreflight(ctx, operation, binding, stepID, 1)
			if err != nil {
				return controller.stopAfterFailure(operation, err)
			}
		}
		stepID = fmt.Sprintf("stage-%02d", index+1)
		if !stepSucceeded(operation, stepID) {
			if recovering {
				refreshID := nextRefreshID(operation, index+1)
				operation, err = controller.runPreflight(ctx, operation, binding, refreshID, 1)
				if err != nil {
					return controller.stopAfterFailure(operation, err)
				}
			}
			operation, err = controller.runStep(ctx, operation, binding, stepSpec{
				id: stepID, phase: journal.PhasePreflight, mode: journal.ModeReadOnly,
				call: binding.stage, reached: binding.stageReached,
			})
			if err != nil {
				return controller.stopAfterFailure(operation, err)
			}
		}
		observed, observeErr := binding.observe(ctx, operation.ID)
		if observeErr != nil {
			return operation, ErrObservation
		}
		rollbackReferences[binding.componentID], err = binding.rollbackReference(observed)
		if err != nil {
			return operation, ErrRecoveryRequired
		}
	}
	for index, binding := range controller.ordered {
		for actionIndex, action := range binding.forward {
			stepID := fmt.Sprintf("apply-%02d-%02d-%s", index+1, actionIndex+1, action.name)
			if stepSucceeded(operation, stepID) {
				continue
			}
			if stepFailed(operation, stepID) {
				return controller.rollback(ctx, operation)
			}
			operation, err = controller.runStep(ctx, operation, binding, stepSpec{
				id: stepID, phase: journal.PhaseApply, mode: journal.ModeMutation,
				rollbackReference: rollbackReferences[binding.componentID], call: action.call,
				recoveryOnly: action.recoveryOnly, reached: action.reached, predecessor: action.predecessor,
			})
			if err != nil {
				if errors.Is(err, ErrRecoveryRequired) || errors.Is(err, ErrObservation) {
					return operation, err
				}
				if hasRecoveryOnlyMutation(operation) {
					finished, finishErr := controller.journal.FinishOperation(operation.ID, journal.OutcomeRecoveryRequired)
					if finishErr != nil {
						return operation, finishErr
					}
					return finished, ErrRecoveryRequired
				}
				return controller.rollback(ctx, operation)
			}
		}
	}
	for index, binding := range controller.ordered {
		stepID := fmt.Sprintf("verify-%02d", index+1)
		if stepSucceeded(operation, stepID) {
			continue
		}
		operation, err = controller.runStep(ctx, operation, binding, stepSpec{
			id: stepID, phase: journal.PhaseVerify, mode: journal.ModeReadOnly,
			call: binding.verifyCandidate.call, reached: binding.verifyCandidate.reached,
		})
		if err != nil {
			if errors.Is(err, ErrRecoveryRequired) || errors.Is(err, ErrObservation) {
				return operation, err
			}
			if hasRecoveryOnlyMutation(operation) {
				finished, finishErr := controller.journal.FinishOperation(operation.ID, journal.OutcomeRecoveryRequired)
				if finishErr != nil {
					return operation, finishErr
				}
				return finished, ErrRecoveryRequired
			}
			return controller.rollback(ctx, operation)
		}
	}
	return controller.journal.FinishOperation(operation.ID, journal.OutcomeComplete)
}

func stepSucceeded(operation journal.Operation, stepID string) bool {
	for _, step := range operation.Steps {
		if step.StepID == stepID && step.Status == journal.StepSucceeded {
			return true
		}
	}
	return false
}

func stepFailed(operation journal.Operation, stepID string) bool {
	for _, step := range operation.Steps {
		if step.StepID == stepID && step.Status == journal.StepFailed {
			return true
		}
	}
	return false
}

func nextRefreshID(operation journal.Operation, componentIndex int) string {
	prefix := fmt.Sprintf("refresh-%02d-", componentIndex)
	count := 1
	for _, step := range operation.Steps {
		if strings.HasPrefix(step.StepID, prefix) {
			count++
		}
	}
	return fmt.Sprintf("%s%02d", prefix, count)
}

func (controller *Controller) runPreflight(ctx context.Context, operation journal.Operation, binding *Binding, stepID string, attempt uint32) (journal.Operation, error) {
	before, err := binding.observe(ctx, operation.ID)
	if err != nil {
		return operation, ErrObservation
	}
	operation, err = controller.journal.BeginStep(journal.StepInput{OperationID: operation.ID, StepID: stepID, ComponentID: binding.componentID, Phase: journal.PhasePreflight, Mode: journal.ModeReadOnly, Attempt: attempt})
	if err != nil {
		return operation, err
	}
	result, callErr := binding.preflight(ctx, operation.ID)
	after, observeErr := binding.observe(ctx, operation.ID)
	if observeErr != nil {
		return operation, ErrRecoveryRequired
	}
	if callErr == nil && bytes.Equal(before, after) && len(result) > 0 {
		return controller.journal.FinishStep(journal.StepResultInput{OperationID: operation.ID, StepID: stepID, Attempt: attempt, Status: journal.StepSucceeded, ResultDigest: digest(result)})
	}
	if callErr != nil && bytes.Equal(before, after) {
		operation, finishErr := controller.journal.FinishStep(journal.StepResultInput{OperationID: operation.ID, StepID: stepID, Attempt: attempt, Status: journal.StepFailed, ReasonCode: reason(callErr)})
		if finishErr != nil {
			return operation, finishErr
		}
		return operation, ErrAdapter
	}
	return operation, ErrRecoveryRequired
}

type stepSpec struct {
	id                string
	attempt           uint32
	phase             journal.Phase
	mode              journal.StepMode
	rollbackReference string
	recoveryOnly      journal.RecoveryOnlyReason
	compensates       string
	call              func(context.Context, string) error
	reached           func([]byte) bool
	predecessor       func([]byte) bool
}

func (controller *Controller) runStep(ctx context.Context, operation journal.Operation, binding *Binding, spec stepSpec) (journal.Operation, error) {
	before, err := binding.observe(ctx, operation.ID)
	if err != nil {
		return operation, ErrObservation
	}
	attempt := spec.attempt
	if attempt == 0 {
		attempt = 1
	}
	input := journal.StepInput{OperationID: operation.ID, StepID: spec.id, ComponentID: binding.componentID, Phase: spec.phase, Mode: spec.mode, Attempt: attempt, CompensatesStepID: spec.compensates}
	if spec.mode == journal.ModeMutation {
		input.IdempotencyDigest = intentDigest(controller.plan.Digest, operation.ID, binding.componentID, spec.id)
		input.Rollback = journal.RollbackPolicy{ReferenceDigest: spec.rollbackReference, RecoveryOnlyReason: spec.recoveryOnly}
		if spec.recoveryOnly != "" {
			input.Rollback.ReferenceDigest = ""
		}
	}
	operation, err = controller.journal.BeginStep(input)
	if err != nil {
		return operation, err
	}
	callErr := spec.call(ctx, operation.ID)
	after, observeErr := binding.observe(ctx, operation.ID)
	if observeErr != nil {
		return operation, ErrRecoveryRequired
	}
	acceptReachedOnError := spec.mode == journal.ModeMutation || spec.phase == journal.PhasePreflight
	if spec.reached(after) && (callErr == nil || acceptReachedOnError) {
		return controller.journal.FinishStep(journal.StepResultInput{OperationID: operation.ID, StepID: spec.id, Attempt: attempt, Status: journal.StepSucceeded, ResultDigest: digest(after)})
	}
	if callErr != nil && (bytes.Equal(before, after) || spec.predecessor != nil && spec.predecessor(after)) {
		operation, finishErr := controller.journal.FinishStep(journal.StepResultInput{OperationID: operation.ID, StepID: spec.id, Attempt: attempt, Status: journal.StepFailed, ReasonCode: reason(callErr)})
		if finishErr != nil {
			return operation, finishErr
		}
		return operation, ErrAdapter
	}
	return operation, ErrRecoveryRequired
}

func (controller *Controller) stopAfterFailure(operation journal.Operation, cause error) (journal.Operation, error) {
	if errors.Is(cause, ErrRecoveryRequired) || errors.Is(cause, ErrObservation) {
		return operation, cause
	}
	finished, err := controller.journal.FinishOperation(operation.ID, journal.OutcomeFailed)
	if err != nil {
		return operation, err
	}
	return finished, cause
}

func (controller *Controller) rollback(ctx context.Context, operation journal.Operation) (journal.Operation, error) {
	if operation.State == journal.StateApplying || operation.State == journal.StateVerifying || operation.State == journal.StateFailed {
		var err error
		operation, err = controller.journal.BeginRollback(operation.ID)
		if err != nil {
			return operation, err
		}
	}
	verified := make(map[string]bool)
	for bindingIndex := len(controller.ordered) - 1; bindingIndex >= 0; bindingIndex-- {
		binding := controller.ordered[bindingIndex]
		for _, actionIndex := range binding.rollbackOrder {
			action := binding.forward[actionIndex]
			original, ok := findApplyStep(operation, binding.componentID, action.name)
			if !ok {
				continue
			}
			rollbackStepID := "rollback-" + original.StepID[len("apply-"):]
			if stepSucceeded(operation, rollbackStepID) {
				continue
			}
			if stepFailed(operation, rollbackStepID) {
				finished, finishErr := controller.journal.FinishOperation(operation.ID, journal.OutcomeRecoveryRequired)
				if finishErr != nil {
					return operation, finishErr
				}
				return finished, ErrRecoveryRequired
			}
			spec := stepSpec{
				id: rollbackStepID, phase: journal.PhaseRollback, mode: journal.ModeMutation,
				rollbackReference: original.Rollback.ReferenceDigest, compensates: original.StepID,
				call: action.reverse, reached: action.reversed,
			}
			var err error
			operation, err = controller.runStep(ctx, operation, binding, spec)
			if err != nil {
				if !errors.Is(err, ErrRecoveryRequired) {
					if finished, finishErr := controller.journal.FinishOperation(operation.ID, journal.OutcomeRecoveryRequired); finishErr == nil {
						operation = finished
					}
				}
				return operation, err
			}
		}
	}
	for index := len(controller.ordered) - 1; index >= 0; index-- {
		binding := controller.ordered[index]
		if !componentMutated(operation, binding.componentID) || verified[binding.componentID] {
			continue
		}
		verifyStepID := fmt.Sprintf("rollback-verify-%02d", index+1)
		if stepSucceeded(operation, verifyStepID) {
			verified[binding.componentID] = true
			continue
		}
		var err error
		operation, err = controller.runStep(ctx, operation, binding, stepSpec{
			id: verifyStepID, phase: journal.PhaseRollback, mode: journal.ModeReadOnly,
			call: binding.verifyPrevious.call, reached: binding.verifyPrevious.reached,
		})
		if err != nil {
			if !errors.Is(err, ErrRecoveryRequired) {
				if finished, finishErr := controller.journal.FinishOperation(operation.ID, journal.OutcomeRecoveryRequired); finishErr == nil {
					operation = finished
				}
			}
			return operation, err
		}
		verified[binding.componentID] = true
	}
	finished, err := controller.journal.FinishOperation(operation.ID, journal.OutcomeRolledBack)
	if err != nil {
		return operation, err
	}
	return finished, ErrAdapter
}

func findApplyStep(operation journal.Operation, componentID, action string) (journal.Step, bool) {
	for _, step := range operation.Steps {
		if step.ComponentID == componentID && step.Phase == journal.PhaseApply && step.Mode == journal.ModeMutation && step.Status != journal.StepRunning && len(step.StepID) >= len(action) && step.StepID[len(step.StepID)-len(action):] == action {
			return step, true
		}
	}
	return journal.Step{}, false
}

func componentMutated(operation journal.Operation, componentID string) bool {
	for _, step := range operation.Steps {
		if step.ComponentID == componentID && step.Phase == journal.PhaseApply && step.Mode == journal.ModeMutation {
			return true
		}
	}
	return false
}

func hasRecoveryOnlyMutation(operation journal.Operation) bool {
	for _, step := range operation.Steps {
		if step.Phase == journal.PhaseApply && step.Mode == journal.ModeMutation && step.Rollback.RecoveryOnlyReason != "" {
			return true
		}
	}
	return false
}

func intentDigest(planDigest, operationID, componentID, stepID string) string {
	encoded, _ := json.Marshal([]string{planDigest, operationID, componentID, stepID})
	return digest(encoded)
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func reason(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "context_cancelled"
	}
	return "adapter_failed"
}
