// Package authentik implements a bounded Authentik 2026.5 server/worker
// runtime mechanism. It grants no controller authority by itself.
package authentik

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
	defaultTimeout   = 30 * time.Second
	transactionsName = "transactions"
	scratchName      = "scratch"
)

const (
	labelAdapter         = "com.gotthstack.adapter"
	labelComponent       = "com.gotthstack.component"
	labelConfiguration   = "com.gotthstack.configuration-digest"
	labelDatabaseSecret  = "com.gotthstack.database-secret-revision"
	labelKeySecret       = "com.gotthstack.key-secret-revision"
	labelOperation       = "com.gotthstack.operation"
	labelRole            = "com.gotthstack.role"
	adapterLabelValue    = "authentik.v1"
	dockerHost           = "unix:///var/run/docker.sock"
	dataTarget           = "/data"
	certsTarget          = "/certs"
	templatesTarget      = "/templates"
	databaseSecretTarget = "/run/secrets/postgresql-password"
	keySecretTarget      = "/run/secrets/secret-key"
)

var (
	ErrInvalidInput     = errors.New("authentik adapter input is invalid")
	ErrUnsafeState      = errors.New("authentik adapter state is unsafe")
	ErrLimit            = errors.New("authentik adapter limit exceeded")
	ErrExecutable       = errors.New("authentik adapter executable rejected")
	ErrEngine           = errors.New("authentik adapter engine failed")
	ErrImage            = errors.New("authentik adapter image rejected")
	ErrContainer        = errors.New("authentik adapter container rejected")
	ErrSecret           = errors.New("authentik adapter secret rejected")
	ErrStorage          = errors.New("authentik adapter storage failed")
	ErrConflict         = errors.New("authentik adapter state conflicts")
	ErrLocked           = errors.New("authentik adapter is locked")
	ErrRecoveryRequired = errors.New("authentik adapter recovery is required")
	ErrClosed           = errors.New("authentik adapter is closed")
)

type Role string

const (
	RoleServer Role = "server"
	RoleWorker Role = "worker"
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
	Image             string `json:"image"`
	DatabaseHost      string `json:"database_host"`
	DatabasePort      uint16 `json:"database_port"`
	Database          string `json:"database"`
	DatabaseRole      string `json:"database_role"`
	HTTPPort          uint16 `json:"http_port"`
	HTTPSPort         uint16 `json:"https_port"`
	WorkerHTTPPort    uint16 `json:"worker_http_port"`
	WorkerHTTPSPort   uint16 `json:"worker_https_port"`
	ServerMetricsPort uint16 `json:"server_metrics_port"`
	WorkerMetricsPort uint16 `json:"worker_metrics_port"`
	UID               uint32 `json:"uid"`
	GID               uint32 `json:"gid"`
}

type Request struct {
	OperationID                  string
	ComponentID                  string
	Specification                Specification
	ConfigurationDigest          string
	DatabaseSecretRevisionDigest string
	KeySecretRevisionDigest      string
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

type RoleObservation struct {
	PrimaryState  State `json:"primary_state"`
	PrimaryPower  Power `json:"primary_power"`
	RollbackState State `json:"rollback_state"`
	RollbackPower Power `json:"rollback_power"`
}

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
	PreviousServerRunning bool   `json:"previous_server_running"`
	PreviousWorkerRunning bool   `json:"previous_worker_running"`
}

type Observation struct {
	Summary    Summary         `json:"summary"`
	StageState State           `json:"stage_state"`
	Server     RoleObservation `json:"server"`
	Worker     RoleObservation `json:"worker"`
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

type fileIdentityJSON struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
}

type managedSpec struct {
	OperationID            string        `json:"operation_id"`
	ComponentID            string        `json:"component_id"`
	ConfigurationDigest    string        `json:"configuration_digest"`
	DatabaseSecretRevision string        `json:"database_secret_revision"`
	KeySecretRevision      string        `json:"key_secret_revision"`
	Specification          Specification `json:"specification"`
	ImageID                string        `json:"image_id"`
}

type transactionMetadata struct {
	SchemaVersion          int              `json:"schema_version"`
	OperationID            string           `json:"operation_id"`
	ComponentID            string           `json:"component_id"`
	BindingDigest          string           `json:"binding_digest"`
	RollbackReference      string           `json:"rollback_reference"`
	EngineDigest           string           `json:"engine_digest"`
	Candidate              managedSpec      `json:"candidate"`
	Previous               *managedSpec     `json:"previous,omitempty"`
	PreviousServerRunning  bool             `json:"previous_server_running"`
	PreviousWorkerRunning  bool             `json:"previous_worker_running"`
	DataIdentity           fileIdentityJSON `json:"data_identity"`
	CertsIdentity          fileIdentityJSON `json:"certs_identity"`
	TemplatesIdentity      fileIdentityJSON `json:"templates_identity"`
	DatabaseSecretIdentity fileIdentityJSON `json:"database_secret_identity"`
	KeySecretIdentity      fileIdentityJSON `json:"key_secret_identity"`
}

type containerRecord struct {
	operationID         string
	componentID         string
	configurationDigest string
	databaseSecret      string
	keySecret           string
	image               string
	imageID             string
	role                Role
	specification       Specification
	running             bool
	id                  string
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
	commandHealth
)

type commandRequest struct {
	kind               commandKind
	name               string
	destination        string
	spec               managedSpec
	role               Role
	timeoutSec         int
	dataPath           string
	certsPath          string
	templatesPath      string
	databaseSecretPath string
	keySecretPath      string
	environment        []string
	workingDir         string
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
