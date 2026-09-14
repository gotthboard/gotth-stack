package authentik

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
)

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
		return createArguments(base, request)
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
	case commandHealth:
		if request.name == "" || !validRole(request.role) {
			return nil, ErrInvalidInput
		}
		return append(base, "exec", request.name, "ak", "healthcheck"), nil
	default:
		return nil, ErrInvalidInput
	}
}

func createArguments(base []string, request commandRequest) ([]string, error) {
	if request.name == "" || !validRole(request.role) || request.dataPath == "" || request.templatesPath == "" || request.databaseSecretPath == "" || request.keySecretPath == "" || request.role == RoleWorker && request.certsPath == "" {
		return nil, ErrInvalidInput
	}
	spec := request.spec
	if err := validateSpecification(spec.Specification); err != nil {
		return nil, err
	}
	uidGID := strconv.FormatUint(uint64(spec.Specification.UID), 10) + ":" + strconv.FormatUint(uint64(spec.Specification.GID), 10)
	metricsPort := spec.Specification.ServerMetricsPort
	httpPort := spec.Specification.HTTPPort
	httpsPort := spec.Specification.HTTPSPort
	if request.role == RoleWorker {
		metricsPort = spec.Specification.WorkerMetricsPort
		httpPort = spec.Specification.WorkerHTTPPort
		httpsPort = spec.Specification.WorkerHTTPSPort
	}
	arguments := append(base,
		"create", "--pull", "never", "--name", request.name,
		"--label", labelAdapter+"="+adapterLabelValue,
		"--label", labelComponent+"="+spec.ComponentID,
		"--label", labelConfiguration+"="+spec.ConfigurationDigest,
		"--label", labelDatabaseSecret+"="+spec.DatabaseSecretRevision,
		"--label", labelKeySecret+"="+spec.KeySecretRevision,
		"--label", labelOperation+"="+spec.OperationID,
		"--label", labelRole+"="+string(request.role),
		"--network", "host", "--user", uidGID, "--read-only",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=67108864",
		"--shm-size", "536870912",
		"--mount", "type=bind,src="+request.dataPath+",dst="+dataTarget,
		"--mount", "type=bind,src="+request.templatesPath+",dst="+templatesTarget+",readonly",
		"--mount", "type=bind,src="+request.databaseSecretPath+",dst="+databaseSecretTarget+",readonly",
		"--mount", "type=bind,src="+request.keySecretPath+",dst="+keySecretTarget+",readonly",
	)
	if request.role == RoleWorker {
		arguments = append(arguments, "--mount", "type=bind,src="+request.certsPath+",dst="+certsTarget)
	}
	arguments = append(arguments,
		"--env", "AUTHENTIK_POSTGRESQL__HOST="+spec.Specification.DatabaseHost,
		"--env", "AUTHENTIK_POSTGRESQL__PORT="+strconv.Itoa(int(spec.Specification.DatabasePort)),
		"--env", "AUTHENTIK_POSTGRESQL__NAME="+spec.Specification.Database,
		"--env", "AUTHENTIK_POSTGRESQL__USER="+spec.Specification.DatabaseRole,
		"--env", "AUTHENTIK_POSTGRESQL__PASSWORD=file://"+databaseSecretTarget,
		"--env", "AUTHENTIK_POSTGRESQL__SSLMODE=disable",
		"--env", "AUTHENTIK_SECRET_KEY=file://"+keySecretTarget,
		"--env", "AUTHENTIK_ERROR_REPORTING__ENABLED=false",
		"--env", "AUTHENTIK_DISABLE_UPDATE_CHECK=true",
		"--env", "AUTHENTIK_LISTEN__HTTP=127.0.0.1:"+strconv.Itoa(int(httpPort)),
		"--env", "AUTHENTIK_LISTEN__HTTPS=127.0.0.1:"+strconv.Itoa(int(httpsPort)),
		"--env", "AUTHENTIK_LISTEN__METRICS=127.0.0.1:"+strconv.Itoa(int(metricsPort)),
		spec.Specification.Image, string(request.role),
	)
	return arguments, nil
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
	request.environment = []string{"PATH=/usr/bin:/bin", "HOME=" + adapter.statePath + "/" + scratchName, "DOCKER_CONFIG=" + adapter.statePath + "/" + scratchName}
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
