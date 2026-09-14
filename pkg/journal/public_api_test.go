package journal_test

import (
	"testing"

	"github.com/gotthboard/gotth-stack/pkg/journal"
	"github.com/gotthboard/gotth-stack/pkg/stack"
)

func TestPublicAPISignatures(t *testing.T) {
	var _ func(string, string) (*journal.Journal, error) = journal.Create
	var _ func(string, string) (*journal.Journal, journal.Recovery, error) = journal.Open
	var _ func(*journal.Journal, stack.Plan, journal.ApprovalInput) (journal.Approval, error) = (*journal.Journal).RecordApproval
	var _ func(*journal.Journal, journal.OperationInput) (journal.Operation, error) = (*journal.Journal).StartOperation
	var _ func(*journal.Journal, journal.StepInput) (journal.Operation, error) = (*journal.Journal).BeginStep
	var _ func(*journal.Journal, journal.StepResultInput) (journal.Operation, error) = (*journal.Journal).FinishStep
	var _ func(*journal.Journal, string) (journal.Operation, error) = (*journal.Journal).Cancel
	var _ func(*journal.Journal, string) (journal.Operation, error) = (*journal.Journal).BeginRollback
	var _ func(*journal.Journal, string, journal.Outcome) (journal.Operation, error) = (*journal.Journal).FinishOperation
	var _ func(*journal.Journal, string) (journal.Operation, error) = (*journal.Journal).Operation
	var _ func(*journal.Journal) error = (*journal.Journal).Close
}
