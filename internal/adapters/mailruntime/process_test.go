package mailruntime

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func runtimeCandidate(role Role) Candidate {
	return Candidate{
		OperationID: "replace-mail", ComponentID: "mail", ProductVersion: "1.0.0-alpha.1",
		SourceCommit: strings.Repeat("b", 40), Image: roleImageRepositories[role] + "@" + testDigest,
		ConfigurationDigest: testDigest, ConfigurationSize: 10240, ConfigurationMembers: []FileArtifact{{Name: "control/config.json", Digest: testDigest, Size: 1}}, SecretRevisionDigest: testDigest,
	}
}

func runtimeDefinition(role Role) roleDefinition {
	definition := roleDefinition{
		role: role, adapterID: "mailruntime." + string(role) + ".v1", repository: roleImageRepositories[role], alias: string(role),
		user:   "1000:1000",
		mounts: []mountSpec{{source: "/srv/config", destination: configTarget, readOnly: true, kind: mountDirectory}},
	}
	if role == RolePostfix || role == RoleDovecot {
		definition.user = "0:0"
		definition.capAdd = []string{"CHOWN", "DAC_OVERRIDE", "DAC_READ_SEARCH", "FOWNER", "NET_BIND_SERVICE", "SETGID", "SETUID"}
	}
	return definition
}

func createRequest(role Role) commandRequest {
	definition := runtimeDefinition(role)
	candidate := runtimeCandidate(role)
	name := "gotth-mail-" + string(role)
	return commandRequest{kind: commandCreate, name: name, definition: definition, spec: managedSpec{Candidate: candidate, Role: role, ContainerName: name, NetworkName: "gotth-private", MountSources: []string{"/srv/config"}}}
}

func TestFixedArgumentsCommonCommands(t *testing.T) {
	cases := []struct {
		request commandRequest
		want    []string
	}{
		{request: commandRequest{kind: commandVersion}, want: []string{"--host", dockerHost, "version", "--format", "{{json .Server}}"}},
		{request: commandRequest{kind: commandNetworkList, name: "gotth-private"}, want: []string{"--host", dockerHost, "network", "ls", "--filter", "name=^gotth-private$", "--format", "{{.Name}}"}},
		{request: commandRequest{kind: commandNetworkInspect, name: "gotth-private"}, want: []string{"--host", dockerHost, "network", "inspect", "gotth-private"}},
		{request: commandRequest{kind: commandNetworkCreate, name: "gotth-private"}, want: []string{"--host", dockerHost, "network", "create", "--driver", "bridge", "--label", "com.gotthstack.network=mailruntime.v1", "gotth-private"}},
		{request: commandRequest{kind: commandContainerList, name: "gotth-mail"}, want: []string{"--host", dockerHost, "container", "ls", "--all", "--filter", "name=^/gotth-mail$", "--format", "{{.Names}}"}},
		{request: commandRequest{kind: commandContainerInspect, name: "gotth-mail"}, want: []string{"--host", dockerHost, "inspect", "gotth-mail"}},
		{request: commandRequest{kind: commandStop, name: "gotth-mail", timeoutSec: 20}, want: []string{"--host", dockerHost, "stop", "--time", "20", "gotth-mail"}},
		{request: commandRequest{kind: commandRename, name: "gotth-mail", destination: "gotth-mail-rollback"}, want: []string{"--host", dockerHost, "rename", "gotth-mail", "gotth-mail-rollback"}},
		{request: commandRequest{kind: commandStart, name: "gotth-mail"}, want: []string{"--host", dockerHost, "start", "gotth-mail"}},
		{request: commandRequest{kind: commandRemove, name: "gotth-mail"}, want: []string{"--host", dockerHost, "rm", "gotth-mail"}},
	}
	for _, tc := range cases {
		got, err := fixedArguments(tc.request)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Fatalf("request=%#v arguments=%q error=%v", tc.request, got, err)
		}
	}
}

func TestCreateArgumentsRoleBoundary(t *testing.T) {
	roles := []Role{RoleControlPlane, RoleDovecot, RoleFront, RolePostfix, RoleRspamd}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			request := createRequest(role)
			arguments, err := fixedArguments(request)
			if err != nil {
				t.Fatal(err)
			}
			joined := " " + strings.Join(arguments, " ") + " "
			for _, required := range []string{
				" --host " + dockerHost + " ", " create ", " --pull never ", " --name " + request.name + " ",
				" --label " + labelAdapter + "=" + request.definition.adapterID + " ",
				" --read-only ", " --cap-drop ALL ", " --security-opt no-new-privileges ",
				" --network gotth-private ", " --network-alias " + string(role) + " ",
				" --mount type=bind,src=/srv/config,dst=" + configTarget + ",readonly ",
				" " + request.spec.Candidate.Image + " ",
			} {
				if !strings.Contains(joined, required) {
					t.Fatalf("missing %q in %q", required, joined)
				}
			}
			for _, forbidden := range []string{" --privileged ", " /var/run/docker.sock ", " docker pull ", "PASSWORD=", "TOKEN="} {
				if strings.Contains(joined, forbidden) {
					t.Fatalf("found forbidden %q in %q", forbidden, joined)
				}
			}
			if (role == RolePostfix || role == RoleDovecot) != strings.Contains(joined, " --cap-add CHOWN ") {
				t.Fatalf("wrong capability policy in %q", joined)
			}
		})
	}
}

func TestFrontArgumentsOwnOnlyPublicMailPorts(t *testing.T) {
	request := createRequest(RoleFront)
	request.definition.ports = []portSpec{{hostIP: "0.0.0.0", hostPort: 25, containerPort: 1025}, {hostIP: "0.0.0.0", hostPort: 465, containerPort: 1465}, {hostIP: "0.0.0.0", hostPort: 587, containerPort: 1587}, {hostIP: "0.0.0.0", hostPort: 143, containerPort: 1143}, {hostIP: "0.0.0.0", hostPort: 993, containerPort: 1993}}
	arguments, err := fixedArguments(request)
	if err != nil {
		t.Fatal(err)
	}
	wanted := []string{"0.0.0.0:25:1025/tcp", "0.0.0.0:465:1465/tcp", "0.0.0.0:587:1587/tcp", "0.0.0.0:143:1143/tcp", "0.0.0.0:993:1993/tcp"}
	var got []string
	for index, value := range arguments {
		if value == "--publish" && index+1 < len(arguments) {
			got = append(got, arguments[index+1])
		}
	}
	if !slices.Equal(got, wanted) {
		t.Fatalf("ports=%q", got)
	}
	for _, forbidden := range []uint16{80, 110, 443, 5432, 11334} {
		if slices.ContainsFunc(got, func(value string) bool { return strings.Contains(value, ":"+strconv.Itoa(int(forbidden))+":") }) {
			t.Fatalf("published forbidden port %d", forbidden)
		}
	}
}

func TestFixedArgumentsRejectsInvalidRequests(t *testing.T) {
	valid := createRequest(RolePostfix)
	cases := []commandRequest{
		{}, {kind: commandContainerList}, {kind: commandContainerInspect}, {kind: commandStop, name: "x", timeoutSec: 0},
		{kind: commandStop, name: "x", timeoutSec: 61}, {kind: commandRename, name: "x"}, {kind: commandStart}, {kind: commandRemove},
		{kind: commandHealth, name: "x"}, {kind: commandImageInspect}, {kind: commandNetworkList}, {kind: commandNetworkInspect}, {kind: commandNetworkCreate},
	}
	brokenName := valid
	brokenName.name = "other"
	cases = append(cases, brokenName)
	brokenNetwork := valid
	brokenNetwork.spec.NetworkName = "host"
	cases = append(cases, brokenNetwork)
	brokenRole := valid
	brokenRole.spec.Role = RoleFront
	cases = append(cases, brokenRole)
	for _, request := range cases {
		if _, err := fixedArguments(request); err != ErrInvalidInput {
			t.Fatalf("request=%#v error=%v", request, err)
		}
	}
}
