# V0 verification evidence

Verification target: `baec0ebc089ff09ec2975ec23130aa9f35f60852`

Host: dedicated `development` host, Linux, Go 1.26.6. The checkout was a fresh
clone of the private Forgejo repository at the exact detached source commit.
No live deployment target or credential was used.

## Passed gates

- `make verify`: exact Go version, formatting, `go vet -mod=readonly`, full
  race/coverage tests, CLI build, and example validate/plan smoke tests.
- `go test -mod=readonly -race -count=100 ./pkg/stack`.
- `go test -mod=readonly -race -shuffle=on -count=20 ./...`.
- `FuzzParseManifest`, 10 seconds: 260,822 executions, 120 new interesting
  inputs, pass.
- `FuzzBuildPlan`, 10 seconds: 4,249,014 executions, 71 new interesting inputs,
  pass.
- Deterministic example plans compared byte-for-byte equal.
- Source scan found no Go apply, arbitrary execution, HTTP client, syscall,
  Docker, or systemd path.
- `git diff --exit-code`, clean status, and `git fsck --strict` passed.
- Independent `stack_test` consumer compiled and passed within the full suite.

## Coverage

- Total statements: 93.0%.
- `pkg/stack`: 94.5%.
- `cmd/gotth-stack`: 84.2%.
- Validation functions are 100% covered, including IDs, artifacts,
  capabilities, uniqueness, graph ordering, and cycle detection.

The remaining lines are fixed-struct JSON marshal failures that cannot be
induced through the public contract, impossible/defensive decoder token
branches, process `main`, output close behavior, and narrow filesystem race
branches. Accepted input, every declared validation class, duplicate/trailing
JSON, size/depth bounds, graph failures, digest consistency, deterministic
encoding, input immutability, redaction, unsafe files, and output failure are
covered. These gaps do not hide an apply or mutation path.
