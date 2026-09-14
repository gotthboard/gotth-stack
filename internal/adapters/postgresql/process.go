package postgresql

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
)

const identityQuery = "SELECT current_setting('server_version_num'), current_database(), current_user, pg_is_in_recovery()"

func (execRunner) Run(ctx context.Context, binary string, request commandRequest, stdout, stderr io.Writer) error {
	arguments, err := fixedArguments(request)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Env = request.environment
	command.Dir = request.workingDir
	command.Stdin = nil
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func fixedArguments(request commandRequest) ([]string, error) {
	base := []string{"--host", dockerHost}
	switch request.kind {
	case commandVersion:
		return append(base, "version", "--format", "{{json .Server}}"), nil
	case commandImageInspect:
		if request.spec.Specification.Image == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "image", "inspect", request.spec.Specification.Image), nil
	case commandContainerList:
		if request.name == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "container", "ls", "--all", "--filter", "name=^/"+request.name+"$", "--format", "{{.Names}}"), nil
	case commandContainerInspect:
		if request.name == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "inspect", request.name), nil
	case commandStop:
		if request.name == "" || request.timeoutSec < 1 || request.timeoutSec > 60 {
			return nil, ErrInvalidInput
		}
		return append(base, "stop", "--time", strconv.Itoa(request.timeoutSec), request.name), nil
	case commandRename:
		if request.name == "" || request.destination == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "rename", request.name, request.destination), nil
	case commandCreate:
		if request.name == "" || request.dataPath == "" || request.secretPath == "" {
			return nil, ErrInvalidInput
		}
		spec := request.spec
		uidGID := strconv.FormatUint(uint64(spec.Specification.UID), 10) + ":" + strconv.FormatUint(uint64(spec.Specification.GID), 10)
		port := "127.0.0.1:" + strconv.Itoa(int(spec.Specification.Port)) + ":5432/tcp"
		arguments := append(base,
			"create",
			"--pull", "never",
			"--name", request.name,
			"--label", labelAdapter+"="+adapterLabelValue,
			"--label", labelComponent+"="+spec.ComponentID,
			"--label", labelConfiguration+"="+spec.ConfigurationDigest,
			"--label", labelSecretRevision+"="+spec.SecretRevision,
			"--label", labelOperation+"="+spec.OperationID,
			"--user", uidGID,
			"--read-only",
			"--cap-drop", "ALL",
			"--security-opt", "no-new-privileges",
			"--publish", port,
			"--mount", "type=bind,src="+request.dataPath+",dst="+dataTarget,
			"--mount", "type=bind,src="+request.secretPath+",dst="+secretTarget+",readonly",
			"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=67108864",
			"--tmpfs", "/var/run/postgresql:rw,noexec,nosuid,nodev,size=16777216",
			"--env", "POSTGRES_DB="+spec.Specification.Database,
			"--env", "POSTGRES_USER="+spec.Specification.Role,
			"--env", "POSTGRES_PASSWORD_FILE="+secretTarget,
			spec.Specification.Image,
		)
		return arguments, nil
	case commandStart:
		if request.name == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "start", request.name), nil
	case commandRemove:
		if request.name == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "rm", request.name), nil
	case commandReady:
		if request.name == "" || !validID(request.spec.Specification.Role) || !validID(request.spec.Specification.Database) {
			return nil, ErrInvalidInput
		}
		return append(base, "exec", request.name, "pg_isready", "--quiet", "--timeout", "5", "--username", request.spec.Specification.Role, "--dbname", request.spec.Specification.Database), nil
	case commandIdentity:
		if request.name == "" || !validID(request.spec.Specification.Role) || !validID(request.spec.Specification.Database) {
			return nil, ErrInvalidInput
		}
		return append(base, "exec", request.name, "psql", "-AtX", "--set", "ON_ERROR_STOP=1", "--username", request.spec.Specification.Role, "--dbname", request.spec.Specification.Database, "--command", identityQuery), nil
	default:
		return nil, ErrInvalidInput
	}
}

type limitedBuffer struct {
	value []byte
	limit int
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	remaining := buffer.limit - len(buffer.value)
	if remaining <= 0 {
		return 0, ErrLimit
	}
	if len(value) > remaining {
		buffer.value = append(buffer.value, value[:remaining]...)
		return remaining, ErrLimit
	}
	buffer.value = append(buffer.value, value...)
	return len(value), nil
}

func (adapter *Adapter) run(ctx context.Context, request commandRequest, capture bool) ([]byte, error) {
	if err := validateBinary(adapter.dockerBinary, adapter.dockerDigest); err != nil {
		return nil, err
	}
	request.environment = []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + adapter.statePath + "/" + scratchName,
		"DOCKER_CONFIG=" + adapter.statePath + "/" + scratchName,
	}
	request.workingDir = adapter.statePath + "/" + scratchName
	stdout := &limitedBuffer{limit: MaxOutputBytes}
	stderr := &limitedBuffer{limit: 64 << 10}
	err := adapter.runner.Run(ctx, adapter.dockerBinary, request, stdout, stderr)
	if errors.Is(err, ErrLimit) || len(stdout.value) >= MaxOutputBytes || len(stderr.value) >= 64<<10 {
		return nil, ErrLimit
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrEngine
	}
	if !capture {
		return nil, nil
	}
	return append([]byte(nil), stdout.value...), nil
}
