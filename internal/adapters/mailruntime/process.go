package mailruntime

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
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
		if request.spec.Candidate.Image == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "image", "inspect", request.spec.Candidate.Image), nil
	case commandNetworkList:
		if request.name == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "network", "ls", "--filter", "name=^"+request.name+"$", "--format", "{{.Name}}"), nil
	case commandNetworkInspect:
		if request.name == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "network", "inspect", request.name), nil
	case commandNetworkCreate:
		if request.name == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "network", "create", "--driver", "bridge", "--label", "com.gotthstack.network=mailruntime.v1", request.name), nil
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
		if request.name == "" || request.definition.role == "" {
			return nil, ErrInvalidInput
		}
		return append(base, "exec", request.name, "/usr/local/bin/gotth-mail-health", string(request.definition.role)), nil
	default:
		return nil, ErrInvalidInput
	}
}

func createArguments(base []string, request commandRequest) ([]string, error) {
	candidate := request.spec.Candidate
	definition := request.definition
	prefix, found := strings.CutSuffix(request.spec.NetworkName, "-private")
	expectedName := prefix + "-" + candidate.ComponentID + "-" + string(definition.role)
	if request.name == "" || request.name != request.spec.ContainerName || request.name != expectedName || !found || !validPrefix(prefix) || definition.role == "" || request.spec.Role != definition.role || !validCandidate(candidate, definition) || len(definition.mounts) == 0 || len(request.spec.MountSources) != len(definition.mounts) {
		return nil, ErrInvalidInput
	}
	arguments := append(base,
		"create", "--pull", "never", "--name", request.name,
		"--label", labelAdapter+"="+definition.adapterID,
		"--label", labelComponent+"="+candidate.ComponentID,
		"--label", labelConfiguration+"="+candidate.ConfigurationDigest,
		"--label", labelConfigSize+"="+strconv.FormatInt(candidate.ConfigurationSize, 10),
		"--label", labelSecretSet+"="+candidate.SecretRevisionDigest,
		"--label", labelOperation+"="+candidate.OperationID,
		"--label", labelVersion+"="+candidate.ProductVersion,
		"--label", labelSourceCommit+"="+candidate.SourceCommit,
		"--user", definition.user, "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--network", request.spec.NetworkName,
		"--network-alias", definition.alias,
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=67108864",
		"--tmpfs", "/run:rw,noexec,nosuid,nodev,size=16777216",
		"--env", "GOTTH_MAIL_CONFIG_DIR="+configTarget,
	)
	for _, capability := range definition.capAdd {
		arguments = append(arguments, "--cap-add", capability)
	}
	for _, port := range definition.ports {
		arguments = append(arguments, "--publish", port.hostIP+":"+strconv.Itoa(int(port.hostPort))+":"+strconv.Itoa(int(port.containerPort))+"/tcp")
	}
	for index, mount := range definition.mounts {
		source := request.spec.MountSources[index]
		if source == "" || mount.destination == "" {
			return nil, ErrInvalidInput
		}
		value := "type=bind,src=" + source + ",dst=" + mount.destination
		if mount.readOnly {
			value += ",readonly"
		}
		arguments = append(arguments, "--mount", value)
	}
	arguments = append(arguments, candidate.Image)
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

func (adapter *adapter) run(ctx context.Context, request commandRequest, capture bool) ([]byte, error) {
	if err := validateBinary(adapter.dockerBinary, adapter.dockerDigest); err != nil {
		return nil, err
	}
	request.environment = []string{"PATH=/usr/bin:/bin", "HOME=" + adapter.statePath + "/scratch", "DOCKER_CONFIG=" + adapter.statePath + "/scratch"}
	request.workingDir = adapter.statePath + "/scratch"
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
