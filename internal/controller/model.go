// Package controller binds admitted plans to durable journal transitions and
// fixed adapter mechanisms. It deliberately exposes no generic command or
// plugin execution surface.
package controller

import (
	"context"
	"errors"
	"sync"

	"github.com/gotthboard/gotth-stack/pkg/journal"
	"github.com/gotthboard/gotth-stack/pkg/stack"
)

const (
	AdapterCaddy            = "gotth-stack-adapter-caddy.v1"
	AdapterPostgreSQL       = "gotth-stack-adapter-postgresql.v1"
	AdapterAuthentik        = "gotth-stack-adapter-authentik.v1"
	AdapterMailControlPlane = "gotth-stack-adapter-mail-control-plane.v1"
	AdapterMailFront        = "gotth-stack-adapter-mail-front.v1"
	AdapterMailPostfix      = "gotth-stack-adapter-mail-postfix.v1"
	AdapterMailDovecot      = "gotth-stack-adapter-mail-dovecot.v1"
	AdapterMailRspamd       = "gotth-stack-adapter-mail-rspamd.v1"
)

var (
	ErrInvalidInput     = errors.New("controller input is invalid")
	ErrAdapter          = errors.New("controller adapter failed")
	ErrObservation      = errors.New("controller observation failed")
	ErrRecoveryRequired = errors.New("controller recovery is required")
)

// Bindings is closed deliberately. New adapter identities require a source
// change and review; callers cannot register a name or implementation string.
type Bindings struct {
	Caddy            *Binding
	PostgreSQL       *Binding
	Authentik        *Binding
	MailControlPlane *Binding
	MailFront        *Binding
	MailPostfix      *Binding
	MailDovecot      *Binding
	MailRspamd       *Binding
}

type Binding struct {
	componentID   string
	adapterID     string
	capabilities  []string
	secretSlots   []string
	secretDigests map[string]string

	observe           func(context.Context, string) ([]byte, error)
	preflight         func(context.Context, string) ([]byte, error)
	stage             func(context.Context, string) error
	reconcileStage    func(context.Context, string) error
	stageReached      func([]byte) bool
	rollbackReference func([]byte) (string, error)
	forward           []transition
	rollbackOrder     []int
	verifyCandidate   transition
	verifyPrevious    transition
}

type transition struct {
	name         string
	call         func(context.Context, string) error
	reverse      func(context.Context, string) error
	reached      func([]byte) bool
	predecessor  func([]byte) bool
	reversed     func([]byte) bool
	recoveryOnly journal.RecoveryOnlyReason
}

type Controller struct {
	mu       sync.Mutex
	journal  *journal.Journal
	plan     stack.Plan
	approval journal.Approval
	bindings map[string]*Binding
	ordered  []*Binding
}

type ExecuteInput struct {
	OperationID string
}
