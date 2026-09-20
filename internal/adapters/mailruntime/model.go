package mailruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

const (
	RuntimeSchemaVersion = 1
	MaxOutputBytes       = 4 << 20
	MaxBinaryBytes       = 256 << 20
	defaultTimeout       = 20 * time.Second
	dockerHost           = "unix:///var/run/docker.sock"
)

const (
	imageLabelRole    = "com.gotth.mail.role"
	imageLabelSource  = "org.opencontainers.image.revision"
	imageLabelVersion = "org.opencontainers.image.version"
)

const (
	labelAdapter       = "com.gotthstack.adapter"
	labelComponent     = "com.gotthstack.component"
	labelConfiguration = "com.gotthstack.configuration-digest"
	labelConfigSize    = "com.gotthstack.configuration-size"
	labelSecretSet     = "com.gotthstack.secret-set-revision"
	labelOperation     = "com.gotthstack.operation"
	labelVersion       = "com.gotthstack.product-version"
	labelSourceCommit  = "com.gotthstack.source-commit"
)

const (
	configTarget          = "/etc/gotth-mail"
	controlDataTarget     = "/var/lib/gotth-mail"
	extensionStateTarget  = "/var/lib/gotth-mail/extensions"
	extensionSecretTarget = "/run/secrets/extensions"
	databaseSecretTarget  = "/run/secrets/database-password"
	masterSecretTarget    = "/run/secrets/master-key"
	certificateTarget     = "/run/gotth-mail/tls/certificate.pem"
	privateKeyTarget      = "/run/gotth-mail/tls/private-key.pem"
	queueTarget           = "/var/spool/postfix"
	mailboxTarget         = "/var/lib/gotth-mail/mail"
	authSecretTarget      = "/run/secrets/control-token"
	rspamdStateTarget     = "/var/lib/rspamd"
	dkimTarget            = "/var/lib/gotth-mail/dkim"
	rspamdSecretTarget    = "/run/secrets/controller-token"
)

var (
	ErrInvalidInput     = errors.New("mail runtime adapter input is invalid")
	ErrUnsafeState      = errors.New("mail runtime adapter state is unsafe")
	ErrLimit            = errors.New("mail runtime adapter limit exceeded")
	ErrExecutable       = errors.New("mail runtime adapter executable rejected")
	ErrEngine           = errors.New("mail runtime adapter engine failed")
	ErrImage            = errors.New("mail runtime adapter image rejected")
	ErrContainer        = errors.New("mail runtime adapter container rejected")
	ErrConfiguration    = errors.New("mail runtime adapter configuration rejected")
	ErrSecret           = errors.New("mail runtime adapter secret rejected")
	ErrStorage          = errors.New("mail runtime adapter storage failed")
	ErrConflict         = errors.New("mail runtime adapter state conflicts")
	ErrLocked           = errors.New("mail runtime adapter is locked")
	ErrRecoveryRequired = errors.New("mail runtime adapter recovery is required")
	ErrClosed           = errors.New("mail runtime adapter is closed")
)

type RuntimeOptions struct {
	DockerBinary       string
	DockerBinaryDigest string
	StateRoot          string
	ConfigurationRoot  string
	ContainerPrefix    string
	Timeout            time.Duration
}

type ControlPlaneOptions struct {
	Runtime             RuntimeOptions
	DataRoot            string
	ExtensionStateRoot  string
	ExtensionSecretRoot string
	DatabaseSecretFile  string
	MasterSecretFile    string
}

type FrontOptions struct {
	Runtime         RuntimeOptions
	CertificateFile string
	PrivateKeyFile  string
}

type PostfixOptions struct {
	Runtime   RuntimeOptions
	QueueRoot string
}

type DovecotOptions struct {
	Runtime        RuntimeOptions
	MailboxRoot    string
	AuthSecretFile string
}

type RspamdOptions struct {
	Runtime              RuntimeOptions
	DataRoot             string
	DKIMRoot             string
	ControllerSecretFile string
}

type Candidate struct {
	OperationID          string         `json:"operation_id"`
	ComponentID          string         `json:"component_id"`
	ProductVersion       string         `json:"product_version"`
	SourceCommit         string         `json:"source_commit"`
	Image                string         `json:"image"`
	ConfigurationDigest  string         `json:"configuration_digest"`
	ConfigurationSize    int64          `json:"configuration_size"`
	ConfigurationMembers []FileArtifact `json:"configuration_members"`
	SecretRevisionDigest string         `json:"secret_revision_digest"`
}

type ControlPlaneRequest struct{ Candidate Candidate }
type FrontRequest struct{ Candidate Candidate }
type PostfixRequest struct{ Candidate Candidate }
type DovecotRequest struct{ Candidate Candidate }
type RspamdRequest struct{ Candidate Candidate }

type ControlPlane struct{ *adapter }
type Front struct{ *adapter }
type Postfix struct{ *adapter }
type Dovecot struct{ *adapter }
type Rspamd struct{ *adapter }

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
	Role                  Role   `json:"role"`
	OperationID           string `json:"operation_id"`
	ComponentID           string `json:"component_id"`
	BindingDigest         string `json:"binding_digest"`
	NetworkDigest         string `json:"network_digest"`
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
	BindingState  State   `json:"binding_state"`
}

type Prepared struct {
	adapter  *adapter
	metadata transactionMetadata
}

type mountKind uint8

const (
	mountDirectory mountKind = iota + 1
	mountRegularFile
)

type mountSpec struct {
	source      string
	destination string
	readOnly    bool
	kind        mountKind
	secret      bool
	revision    bool
}

type portSpec struct {
	hostIP        string
	hostPort      uint16
	containerPort uint16
}

type roleDefinition struct {
	role       Role
	adapterID  string
	repository string
	alias      string
	user       string
	capAdd     []string
	mounts     []mountSpec
	ports      []portSpec
}

type managedSpec struct {
	Candidate     Candidate `json:"candidate"`
	Role          Role      `json:"role"`
	ImageID       string    `json:"image_id"`
	ContainerName string    `json:"container_name"`
	NetworkName   string    `json:"network_name"`
	MountSources  []string  `json:"mount_sources"`
}

type fileIdentityJSON struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
}

type mountIdentity struct {
	Destination string           `json:"destination"`
	Identity    fileIdentityJSON `json:"identity"`
}

type transactionMetadata struct {
	SchemaVersion     int             `json:"schema_version"`
	Role              Role            `json:"role"`
	OperationID       string          `json:"operation_id"`
	ComponentID       string          `json:"component_id"`
	BindingDigest     string          `json:"binding_digest"`
	NetworkDigest     string          `json:"network_digest"`
	RollbackReference string          `json:"rollback_reference"`
	EngineDigest      string          `json:"engine_digest"`
	Mounts            []mountIdentity `json:"mounts"`
	Candidate         managedSpec     `json:"candidate"`
	Previous          *managedSpec    `json:"previous,omitempty"`
	PreviousRunning   bool            `json:"previous_running"`
}

type containerRecord struct {
	spec    managedSpec
	running bool
	id      string
}

type NetworkSummary struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

type commandKind uint8

const (
	commandVersion commandKind = iota + 1
	commandImageInspect
	commandNetworkList
	commandNetworkInspect
	commandNetworkCreate
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
	kind        commandKind
	name        string
	destination string
	spec        managedSpec
	definition  roleDefinition
	timeoutSec  int
	environment []string
	workingDir  string
}

type commandRunner interface {
	Run(context.Context, string, commandRequest, io.Writer, io.Writer) error
}

type adapter struct {
	mu           sync.Mutex
	dockerBinary string
	dockerDigest string
	statePath    string
	configPath   string
	prefix       string
	network      string
	timeout      time.Duration
	definition   roleDefinition
	runner       commandRunner
	stateRoot    *os.Root
	transactions *os.Root
	lock         *os.File
	mountFiles   []*os.File
	closed       bool
}

type execRunner struct{}

func (adapter *adapter) boundedContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrInvalidInput
	}
	bounded, cancel := context.WithTimeout(ctx, adapter.timeout)
	return bounded, cancel, nil
}
