package controller

import (
	"slices"

	"github.com/gotthboard/gotth-stack/pkg/journal"
	"github.com/gotthboard/gotth-stack/pkg/stack"
)

type bindingSlot struct {
	id      string
	binding *Binding
}

func New(j *journal.Journal, plan stack.Plan, input journal.ApprovalInput, values Bindings) (*Controller, error) {
	if j == nil {
		return nil, ErrInvalidInput
	}
	if _, err := stack.MarshalPlan(plan); err != nil {
		return nil, ErrInvalidInput
	}
	slots := []bindingSlot{
		{AdapterCaddy, values.Caddy},
		{AdapterPostgreSQL, values.PostgreSQL},
		{AdapterAuthentik, values.Authentik},
		{AdapterMailControlPlane, values.MailControlPlane},
		{AdapterMailFront, values.MailFront},
		{AdapterMailPostfix, values.MailPostfix},
		{AdapterMailDovecot, values.MailDovecot},
		{AdapterMailRspamd, values.MailRspamd},
	}
	byAdapter := make(map[string]*Binding, len(slots))
	for _, slot := range slots {
		if slot.binding == nil {
			continue
		}
		if slot.binding.adapterID != slot.id || !validBinding(slot.binding) {
			return nil, ErrInvalidInput
		}
		byAdapter[slot.id] = slot.binding
	}
	if len(byAdapter) != len(plan.Steps) {
		return nil, ErrInvalidInput
	}
	byComponent := make(map[string]*Binding, len(plan.Steps))
	ordered := make([]*Binding, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		binding := byAdapter[step.Adapter]
		if binding == nil || binding.componentID != step.ComponentID || binding.artifactDigest != step.Artifact.Digest || binding.configurationDigest != step.ConfigurationDigest || !slices.Equal(step.Capabilities, binding.capabilities) || !slices.Equal(step.SecretSlots, binding.secretSlots) {
			return nil, ErrInvalidInput
		}
		if _, exists := byComponent[step.ComponentID]; exists {
			return nil, ErrInvalidInput
		}
		byComponent[step.ComponentID] = binding
		ordered = append(ordered, binding)
	}
	if !recoveryPolicyIsolated(ordered) {
		return nil, ErrInvalidInput
	}
	if !revisionsMatch(plan, input.SecretRevisions, byComponent) {
		return nil, ErrInvalidInput
	}
	approval, err := j.RecordApproval(plan, input)
	if err != nil {
		return nil, err
	}
	return &Controller{journal: j, plan: plan, approval: approval, bindings: byComponent, ordered: ordered}, nil
}

func recoveryPolicyIsolated(bindings []*Binding) bool {
	recoveryOnly := 0
	rollbackable := 0
	for _, binding := range bindings {
		for _, action := range binding.forward {
			if action.recoveryOnly == "" {
				rollbackable++
			} else {
				recoveryOnly++
			}
		}
	}
	return recoveryOnly == 0 || (rollbackable == 0 && len(bindings) == 1)
}

func validBinding(value *Binding) bool {
	if value.componentID == "" || value.adapterID == "" || value.artifactDigest == "" || value.configurationDigest == "" || value.observe == nil || value.preflight == nil || value.stage == nil || value.reconcileStage == nil || value.stageReached == nil || value.rollbackReference == nil || value.verifyCandidate.call == nil || value.verifyCandidate.reached == nil || value.verifyPrevious.call == nil || value.verifyPrevious.reached == nil {
		return false
	}
	if !sortedUnique(value.capabilities) || !sortedUnique(value.secretSlots) || len(value.secretDigests) != len(value.secretSlots) {
		return false
	}
	for _, slot := range value.secretSlots {
		if value.secretDigests[slot] == "" {
			return false
		}
	}
	rollbackable := 0
	for _, item := range value.forward {
		if item.recoveryOnly == "" {
			rollbackable++
		}
	}
	if len(value.rollbackOrder) != rollbackable {
		return false
	}
	seenRollback := make([]bool, len(value.forward))
	for _, index := range value.rollbackOrder {
		if index < 0 || index >= len(value.forward) || seenRollback[index] || value.forward[index].recoveryOnly != "" {
			return false
		}
		seenRollback[index] = true
	}
	for _, item := range value.forward {
		rollbackValid := item.recoveryOnly == "" && item.reverse != nil && item.reversed != nil
		recoveryValid := (item.recoveryOnly == journal.RecoveryNoRollback || item.recoveryOnly == journal.RecoveryExternal) && item.reverse == nil && item.reversed == nil
		if item.name == "" || item.call == nil || item.reached == nil || item.predecessor == nil || (!rollbackValid && !recoveryValid) {
			return false
		}
	}
	return len(value.forward) > 0
}

func sortedUnique(values []string) bool {
	if !slices.IsSorted(values) {
		return false
	}
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return false
		}
	}
	return true
}

func revisionsMatch(plan stack.Plan, revisions []journal.SecretRevision, bindings map[string]*Binding) bool {
	if revisions == nil {
		return false
	}
	byKey := make(map[string]string, len(revisions))
	for _, revision := range revisions {
		key := revision.ComponentID + "\x00" + revision.Slot
		if _, exists := byKey[key]; exists {
			return false
		}
		byKey[key] = revision.RevisionDigest
	}
	wanted := 0
	for _, step := range plan.Steps {
		binding := bindings[step.ComponentID]
		for _, slot := range step.SecretSlots {
			digest := binding.secretDigests[slot]
			if digest == "" || byKey[step.ComponentID+"\x00"+slot] != digest {
				return false
			}
			wanted++
		}
	}
	return wanted == len(revisions)
}
