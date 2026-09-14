package journal

import (
	"reflect"
	"sort"

	"github.com/gotthboard/gotth-stack/pkg/stack"
)

// RecordApproval projects every execution-relevant plan field and every
// secret revision into one durable authority record. Secret values have no
// representation in this package.
func (journal *Journal) RecordApproval(plan stack.Plan, input ApprovalInput) (Approval, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.usable(); err != nil {
		return Approval{}, err
	}
	if err := validateApprovalInput(input); err != nil {
		return Approval{}, err
	}
	if _, err := stack.MarshalPlan(plan); err != nil {
		return Approval{}, ErrInvalidInput
	}
	if !secretRevisionsMatchPlan(plan, input.SecretRevisions) {
		return Approval{}, ErrInvalidInput
	}
	now := observedTime(journal.now())
	approval := Approval{
		SchemaVersion: SchemaVersion, ID: input.ID, InstallationID: journal.installationID,
		PlanDigest: plan.Digest, ManifestDigest: plan.ManifestDigest, ActorID: input.ActorID,
		AuthorityDigest: input.AuthorityDigest, IssuedAt: now, ExpiresAt: observedTime(input.ExpiresAt),
		Components:      make([]ApprovedComponent, len(plan.Steps)),
		SecretRevisions: append([]SecretRevision{}, input.SecretRevisions...),
	}
	for index, step := range plan.Steps {
		approval.Components[index] = ApprovedComponent{
			Index: step.Index, ComponentID: step.ComponentID, Adapter: step.Adapter,
			ArtifactDigest: step.Artifact.Digest, ConfigurationDigest: step.ConfigurationDigest,
			Capabilities: append([]string(nil), step.Capabilities...),
		}
	}
	if existing, ok := journal.approvals[input.ID]; ok {
		approval.IssuedAt = existing.IssuedAt
		if reflect.DeepEqual(existing, approval) {
			return cloneApproval(existing), nil
		}
		return Approval{}, ErrConflict
	}
	if !now.Before(approval.ExpiresAt) {
		return Approval{}, ErrExpired
	}
	record := journalRecord{Kind: recordApproval, ObservedAt: now, Approval: &approval}
	if err := journal.preview(record); err != nil {
		return Approval{}, err
	}
	persisted, err := journal.appendRecord(record)
	if err != nil {
		return Approval{}, err
	}
	if err := journal.applyRecord(persisted); err != nil {
		journal.poisoned = true
		return Approval{}, ErrRecoveryRequired
	}
	return cloneApproval(approval), nil
}

// secretRevisionsMatchPlan compares canonical component/slot pairs, not just
// counts, so an approval cannot silently omit or substitute a secret binding.
func secretRevisionsMatchPlan(plan stack.Plan, revisions []SecretRevision) bool {
	expected := make([]string, 0)
	for _, step := range plan.Steps {
		for _, slot := range step.SecretSlots {
			expected = append(expected, step.ComponentID+"\x00"+slot)
		}
	}
	sort.Strings(expected)
	if len(expected) != len(revisions) {
		return false
	}
	for index, revision := range revisions {
		if secretKey(revision) != expected[index] {
			return false
		}
	}
	return true
}
