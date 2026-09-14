package journal

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	slugPattern       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	adapterPattern    = regexp.MustCompile(`^gotth-stack-adapter-[a-z0-9]+(?:-[a-z0-9]+)*\.v[1-9][0-9]*$`)
	capabilityPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:\.[a-z0-9]+(?:-[a-z0-9]+)*)*$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	reasonPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9_]{0,78}[a-z0-9])?$`)
)

func validID(value string) bool { return len(value) <= 80 && slugPattern.MatchString(value) }

func validDigest(value string) bool { return digestPattern.MatchString(value) }

func validReason(value string) bool { return reasonPattern.MatchString(value) }

func observedTime(value time.Time) time.Time { return value.UTC().Round(0) }

// validateApprovalInput bounds every field before it can enter a durable
// record. It also establishes one canonical ordering for secret bindings.
func validateApprovalInput(input ApprovalInput) error {
	if !validID(input.ID) || !validID(input.ActorID) || !validDigest(input.AuthorityDigest) || input.ExpiresAt.IsZero() {
		return ErrInvalidInput
	}
	if input.SecretRevisions == nil || len(input.SecretRevisions) > 4096 {
		return ErrInvalidInput
	}
	for index, item := range input.SecretRevisions {
		if !validID(item.ComponentID) || !validID(item.Slot) || !validDigest(item.RevisionDigest) {
			return ErrInvalidInput
		}
		if index > 0 && secretKey(input.SecretRevisions[index-1]) >= secretKey(item) {
			return ErrInvalidInput
		}
	}
	return nil
}

func validateOperationInput(input OperationInput) error {
	if !validID(input.ID) || !validID(input.ApprovalID) || !validDigest(input.PlanDigest) {
		return ErrInvalidInput
	}
	return nil
}

// validateStepInput rejects ambiguous phase/mode combinations and keeps
// rollback authority represented only by digests or a closed reason enum.
func validateStepInput(input StepInput) error {
	if !validID(input.OperationID) || !validID(input.StepID) || !validID(input.ComponentID) || input.Attempt == 0 {
		return ErrInvalidInput
	}
	if input.Phase == PhasePreflight && input.Mode != ModeReadOnly {
		return ErrInvalidInput
	}
	if (input.Phase == PhaseApply || input.Phase == PhaseRollback) && input.Mode != ModeMutation {
		return ErrInvalidInput
	}
	if input.Phase == PhaseVerify && input.Mode != ModeReadOnly {
		return ErrInvalidInput
	}
	if input.Phase != PhasePreflight && input.Phase != PhaseApply && input.Phase != PhaseVerify && input.Phase != PhaseRollback {
		return ErrInvalidInput
	}
	if input.Mode == ModeMutation {
		if !validDigest(input.IdempotencyDigest) {
			return ErrInvalidInput
		}
		hasReference := validDigest(input.Rollback.ReferenceDigest) && input.Rollback.RecoveryOnlyReason == ""
		hasReason := input.Rollback.ReferenceDigest == "" && (input.Rollback.RecoveryOnlyReason == RecoveryNoRollback || input.Rollback.RecoveryOnlyReason == RecoveryExternal)
		if !hasReference && !hasReason {
			return ErrInvalidInput
		}
	} else if input.IdempotencyDigest != "" || input.Rollback != (RollbackPolicy{}) {
		return ErrInvalidInput
	}
	if input.Phase == PhaseRollback {
		if !validID(input.CompensatesStepID) || input.Rollback.RecoveryOnlyReason != "" {
			return ErrInvalidInput
		}
	} else if input.CompensatesStepID != "" {
		return ErrInvalidInput
	}
	return nil
}

func validateStepResultInput(input StepResultInput) error {
	if !validID(input.OperationID) || !validID(input.StepID) || input.Attempt == 0 {
		return ErrInvalidInput
	}
	if input.Status == StepSucceeded {
		if !validDigest(input.ResultDigest) || input.ReasonCode != "" {
			return ErrInvalidInput
		}
		return nil
	}
	if input.Status == StepFailed && input.ResultDigest == "" && validReason(input.ReasonCode) {
		return nil
	}
	return ErrInvalidInput
}

func secretKey(value SecretRevision) string { return value.ComponentID + "\x00" + value.Slot }

func sortedUnique(values []string) bool {
	return values != nil && sort.StringsAreSorted(values) && !hasAdjacentDuplicate(values)
}

func hasAdjacentDuplicate(values []string) bool {
	for index := 1; index < len(values); index++ {
		if strings.Compare(values[index-1], values[index]) >= 0 {
			return true
		}
	}
	return false
}
