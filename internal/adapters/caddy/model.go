// Package caddy implements the bounded Caddy configuration mechanism used by
// the future GOTTH Stack controller. It grants no deployment authority itself.
package caddy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	SchemaVersion      = 1
	MaxConfigBytes     = 1 << 20
	MaxJSONBytes       = 4 << 20
	MaxCommandOutput   = 64 << 10
	MaxBinaryBytes     = 256 << 20
	defaultTimeout     = 10 * time.Second
	transactionDirName = "transactions"
	scratchDirName     = "scratch"
)

var (
	ErrInvalidInput     = errors.New("caddy adapter input is invalid")
	ErrUnsafeState      = errors.New("caddy adapter state is unsafe")
	ErrLimit            = errors.New("caddy adapter limit exceeded")
	ErrValidation       = errors.New("caddy configuration validation failed")
	ErrCommand          = errors.New("caddy command failed")
	ErrRuntime          = errors.New("caddy runtime observation failed")
	ErrStorage          = errors.New("caddy adapter storage failed")
	ErrConflict         = errors.New("caddy adapter state conflicts")
	ErrLocked           = errors.New("caddy adapter is locked")
	ErrRecoveryRequired = errors.New("caddy adapter recovery is required")
	ErrClosed           = errors.New("caddy adapter is closed")
)

type Options struct {
	CaddyBinary       string
	CaddyBinaryDigest string
	ConfigPath        string
	StateRoot         string
	WorkingDirectory  string
	AdminURL          string
	Timeout           time.Duration
}

type Request struct {
	OperationID    string
	ComponentID    string
	Configuration  []byte
	ExpectedDigest string
}

type DesiredState string

const (
	DesiredCandidate DesiredState = "candidate"
	DesiredPrevious  DesiredState = "previous"
)

type State string

const (
	StateAbsent     State = "absent"
	StatePresent    State = "present"
	StatePrevious   State = "previous"
	StateCandidate  State = "candidate"
	StateOther      State = "other"
	StateIncomplete State = "incomplete"
)

type Summary struct {
	SchemaVersion          int    `json:"schema_version"`
	OperationID            string `json:"operation_id"`
	ComponentID            string `json:"component_id"`
	BindingDigest          string `json:"binding_digest"`
	RollbackReference      string `json:"rollback_reference"`
	PreviousFileDigest     string `json:"previous_file_digest"`
	CandidateFileDigest    string `json:"candidate_file_digest"`
	PreviousRuntimeDigest  string `json:"previous_runtime_digest"`
	CandidateRuntimeDigest string `json:"candidate_runtime_digest"`
}

type Observation struct {
	Summary       Summary `json:"summary"`
	StageState    State   `json:"stage_state"`
	FileState     State   `json:"file_state"`
	FileTemporary State   `json:"file_temporary"`
	RuntimeState  State   `json:"runtime_state"`
	FileDigest    string  `json:"file_digest"`
	RuntimeDigest string  `json:"runtime_digest"`
}

type Prepared struct {
	adapter   *Adapter
	metadata  transactionMetadata
	previous  []byte
	candidate []byte
	target    fileIdentity
}

type commandKind uint8

const (
	commandAdapt commandKind = iota + 1
	commandValidate
	commandReload
)

type commandRequest struct {
	kind             commandKind
	configPath       string
	adminAddress     string
	workingDirectory string
	environment      []string
}

type fileIdentity struct {
	mode          os.FileMode
	device, inode uint64
	uid, gid      int
}

type commandRunner interface {
	Run(context.Context, string, commandRequest, io.Writer, io.Writer) error
}

type runtimeReader interface {
	Read(context.Context) ([]byte, error)
}

type storageOps struct {
	chown    func(*os.File, int, int) error
	chmod    func(*os.File, os.FileMode) error
	write    func(*os.File, []byte) error
	sync     func(*os.File) error
	close    func(*os.File) error
	rename   func(*os.Root, string, string) error
	syncRoot func(*os.Root) error
}

type Adapter struct {
	mu               sync.Mutex
	binary           string
	binaryDigest     string
	configPath       string
	configName       string
	statePath        string
	adminURL         string
	adminAddress     string
	workingDirectory string
	timeout          time.Duration
	configRoot       *os.Root
	stateRoot        *os.Root
	transactionsRoot *os.Root
	lock             *os.File
	runner           commandRunner
	runtime          runtimeReader
	ops              storageOps
	closed           bool
}

type execRunner struct{}

type adminReader struct {
	url    string
	client *http.Client
}

func (adapter *Adapter) boundedContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrInvalidInput
	}
	bounded, cancel := context.WithTimeout(ctx, adapter.timeout)
	return bounded, cancel, nil
}

type transactionMetadata struct {
	SchemaVersion          int    `json:"schema_version"`
	OperationID            string `json:"operation_id"`
	ComponentID            string `json:"component_id"`
	BindingDigest          string `json:"binding_digest"`
	PreviousFileDigest     string `json:"previous_file_digest"`
	CandidateFileDigest    string `json:"candidate_file_digest"`
	PreviousRuntimeDigest  string `json:"previous_runtime_digest"`
	CandidateRuntimeDigest string `json:"candidate_runtime_digest"`
	ConfigMode             uint32 `json:"config_mode"`
	ConfigUID              uint32 `json:"config_uid"`
	ConfigGID              uint32 `json:"config_gid"`
	RollbackReference      string `json:"rollback_reference"`
}
