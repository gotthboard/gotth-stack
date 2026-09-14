package caddy

import (
	"context"
	"errors"
	"io"
	"os/exec"
)

func (execRunner) Run(ctx context.Context, binary string, request commandRequest, stdout, stderr io.Writer) error {
	arguments, err := fixedArguments(request)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Env = request.environment
	command.Dir = request.workingDirectory
	command.Stdin = nil
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func fixedArguments(request commandRequest) ([]string, error) {
	if request.configPath == "" || request.workingDirectory == "" {
		return nil, ErrInvalidInput
	}
	switch request.kind {
	case commandAdapt:
		return []string{"adapt", "--config", request.configPath, "--adapter", "caddyfile"}, nil
	case commandValidate:
		return []string{"validate", "--config", request.configPath, "--adapter", "caddyfile"}, nil
	case commandReload:
		if request.adminAddress == "" {
			return nil, ErrInvalidInput
		}
		return []string{"reload", "--config", request.configPath, "--adapter", "caddyfile", "--address", request.adminAddress}, nil
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
	if err := validateBinary(adapter.binary, adapter.binaryDigest); err != nil {
		return nil, err
	}
	stdoutLimit := MaxCommandOutput
	if capture {
		stdoutLimit = MaxJSONBytes + 1
	}
	stdout := &limitedBuffer{limit: stdoutLimit}
	stderr := &limitedBuffer{limit: MaxCommandOutput}
	err := adapter.runner.Run(ctx, adapter.binary, request, stdout, stderr)
	if errors.Is(err, ErrLimit) || len(stdout.value) >= stdoutLimit || len(stderr.value) >= MaxCommandOutput {
		return nil, ErrLimit
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrCommand
	}
	if !capture {
		return nil, nil
	}
	return append([]byte(nil), stdout.value...), nil
}
