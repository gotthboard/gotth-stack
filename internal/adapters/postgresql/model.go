// Package postgresql implements a bounded PostgreSQL 17 container runtime
// mechanism. It grants no controller authority by itself.
package postgresql

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

const (
	SchemaVersion    = 1
	MaxOutputBytes   = 4 << 20
	MaxSecretBytes   = 4 << 10
	MaxBinaryBytes   = 256 << 20
	defaultTimeout   = 20 * time.Second
	transactionsName = "transactions"
	scratchName      = "scratch"
)

const (
	labelAdapter        = "com.gotthstack.adapter"
	labelComponent      = "com.gotthstack.component"
	labelConfiguration  = "com.gotthstack.configuration-digest"
	labelSecretRevision = "com.gotthstack.secret-revision"
	labelOperation      = "com.gotthstack.operation"
	adapterLabelValue   = "postgresql.v1"
	dockerHost          = "unix:///var/run/docker.sock"
	dataTarget          = "/var/lib/postgresql/data"
	secretTarget        = "/run/secrets/postgres-password"
)

var (
	ErrInvalidInput     = errors.New("postgresql adapter input is invalid")
	ErrUnsafeState      = errors.New("postgresql adapter state is unsafe")
	ErrLimit            = errors.New("postgresql adapter limit exceeded")
	ErrExecutable       = errors.New("postgresql adapter executable rejected")
	ErrEngine           = errors.New("postgresql adapter engine failed")
	ErrImage            = errors.New("postgresql adapter image rejected")
	ErrContainer        = errors.New("postgresql adapter container rejected")
	ErrSecret           = errors.New("postgresql adapter secret rejected")
	ErrStorage          = errors.New("postgresql adapter storage failed")
	ErrConflict         = errors.New("postgresql adapter state conflicts")
	ErrLocked           = errors.New("postgresql adapter is locked")
	ErrRecoveryRequired = errors.New("postgresql adapter recovery is required")
	ErrClosed           = errors.New("postgresql adapter is closed")
)

type Options struct {
	DockerBinary       string
	DockerBinaryDigest string
	StateRoot          string
	DataRoot           string
	SecretRoot         string
	ContainerPrefix    string
	Timeout            time.Duration
}

type Specification struct {
	Image    string `json:"image"`
	Database string `json:"database"`
	Role     string `json:"role"`
	Port     uint16 `json:"port"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
}

type Request struct {
	OperationID          string
	ComponentID          string
	Specification        Specification
	ConfigurationDigest  string
	SecretRevisionDigest string
}

type DesiredState string

const (
	DesiredCandidate DesiredState = "candidate"
	DesiredPrevious  DesiredState = "previous"
)

type State string

const (
	StateAbsent     State = "absent"
	StatePrevious   State = "previous"
	StateCandidate  State = "candidate"
	StateOther      State = "other"
	StateIncomplete State = "incomplete"
)

type Power string

const (
	PowerAbsent  Power = "absent"
	PowerStopped Power = "stopped"
	PowerRunning Power = "running"
)

type Summary struct {
	SchemaVersion         int    `json:"schema_version"`
	OperationID           string `json:"operation_id"`
	ComponentID           string `json:"component_id"`
	BindingDigest         string `json:"binding_digest"`
	RollbackReference     string `json:"rollback_reference"`
	CandidateConfigDigest string `json:"candidate_configuration_digest"`
	PreviousConfigDigest  string `json:"previous_configuration_digest,omitempty"`
	CandidateImageDigest  string `json:"candidate_image_digest"`
	PreviousImageDigest   string `json:"previous_image_digest,omitempty"`
	HadPrevious           bool   `json:"had_previous"`
	PreviousRunning       bool   `json:"previous_running"`
}

type Observation struct {
	Summary       Summary `json:"summary"`
	StageState    State   `json:"stage_state"`
	PrimaryState  State   `json:"primary_state"`
	PrimaryPower  Power   `json:"primary_power"`
	RollbackState State   `json:"rollback_state"`
	RollbackPower Power   `json:"rollback_power"`
	DataState     State   `json:"data_state"`
}

type Prepared struct {
	adapter  *Adapter
	metadata transactionMetadata
}

type fileIdentity struct {
	device uint64
	inode  uint64
	uid    uint32
	gid    uint32
	mode   uint32
	size   int64
}

type managedSpec struct {
	OperationID         string        `json:"operation_id"`
	ComponentID         string        `json:"component_id"`
	ConfigurationDigest string        `json:"configuration_digest"`
	SecretRevision      string        `json:"secret_revision"`
	Specification       Specification `json:"specification"`
	ImageID             string        `json:"image_id"`
	ContainerName       string        `json:"container_name"`
	DataName            string        `json:"data_name"`
	SecretName          string        `json:"secret_name"`
}

type transactionMetadata struct {
	SchemaVersion     int              `json:"schema_version"`
	OperationID       string           `json:"operation_id"`
	ComponentID       string           `json:"component_id"`
	BindingDigest     string           `json:"binding_digest"`
	RollbackReference string           `json:"rollback_reference"`
	EngineDigest      string           `json:"engine_digest"`
	Candidate         managedSpec      `json:"candidate"`
	Previous          *managedSpec     `json:"previous,omitempty"`
	PreviousRunning   bool             `json:"previous_running"`
	DataInitialized   bool             `json:"data_initialized"`
	DataIdentity      fileIdentityJSON `json:"data_identity"`
	SecretIdentity    fileIdentityJSON `json:"secret_identity"`
}

type fileIdentityJSON struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
}

type containerRecord struct {
	spec    managedSpec
	running bool
	id      string
}

type commandKind uint8

const (
	commandVersion commandKind = iota + 1
	commandImageInspect
	commandContainerList
	commandContainerInspect
	commandStop
	commandRename
	commandCreate
	commandStart
	commandRemove
	commandReady
	commandIdentity
)

type commandRequest struct {
	kind        commandKind
	name        string
	destination string
	spec        managedSpec
	timeoutSec  int
	dataPath    string
	secretPath  string
	environment []string
	workingDir  string
}

type commandRunner interface {
	Run(context.Context, string, commandRequest, io.Writer, io.Writer) error
}

type Adapter struct {
	mu               sync.Mutex
	dockerBinary     string
	dockerDigest     string
	statePath        string
	dataPath         string
	secretPath       string
	prefix           string
	timeout          time.Duration
	stateRoot        *os.Root
	dataRoot         *os.Root
	secretRoot       *os.Root
	transactionsRoot *os.Root
	lock             *os.File
	runner           commandRunner
	closed           bool
}

type execRunner struct{}

func (adapter *Adapter) boundedContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrInvalidInput
	}
	bounded, cancel := context.WithTimeout(ctx, adapter.timeout)
	return bounded, cancel, nil
}
