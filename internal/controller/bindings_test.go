package controller

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotthboard/gotth-stack/internal/adapters/authentik"
	"github.com/gotthboard/gotth-stack/internal/adapters/caddy"
	"github.com/gotthboard/gotth-stack/internal/adapters/mailruntime"
	"github.com/gotthboard/gotth-stack/internal/adapters/postgresql"
	"github.com/gotthboard/gotth-stack/pkg/journal"
)

type bindingPaths struct {
	t    *testing.T
	root string
}

func newBindingPaths(t *testing.T) bindingPaths { return bindingPaths{t: t, root: t.TempDir()} }

func (paths bindingPaths) directory(name string) string {
	path := filepath.Join(paths.root, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		paths.t.Fatal(err)
	}
	return path
}

func (paths bindingPaths) file(name string) string {
	path := filepath.Join(paths.root, name)
	if err := os.WriteFile(path, []byte("secret\n"), 0o400); err != nil {
		paths.t.Fatal(err)
	}
	return path
}

func binaryDigest(t *testing.T) string {
	t.Helper()
	value, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(value))
}

func TestConcreteCaddyBindingUsesExactStatePredicates(t *testing.T) {
	paths := newBindingPaths(t)
	configRoot, state, work := paths.directory("config"), paths.directory("state"), paths.directory("work")
	config := filepath.Join(configRoot, "Caddyfile")
	contents := []byte("{}\n")
	if err := os.WriteFile(config, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	adapter, err := caddy.Open(caddy.Options{CaddyBinary: "/usr/bin/true", CaddyBinaryDigest: binaryDigest(t), ConfigPath: config, StateRoot: state, WorkingDirectory: work, AdminURL: "http://127.0.0.1:2019", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	binding, err := BindCaddy("proxy", adapter, caddy.Request{ComponentID: "proxy", Configuration: contents, ExpectedDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(contents))})
	if err != nil || !validBinding(binding) {
		t.Fatalf("binding=%#v err=%v", binding, err)
	}
	previous := caddy.Observation{Summary: caddy.Summary{RollbackReference: testDigest("rollback")}, StageState: caddy.StatePresent, FileState: caddy.StatePrevious, FileTemporary: caddy.StateAbsent, RuntimeState: caddy.StatePrevious}
	installed := previous
	installed.FileState = caddy.StateCandidate
	candidate := previous
	candidate.FileState, candidate.RuntimeState = caddy.StateCandidate, caddy.StateCandidate
	previousJSON, _ := json.Marshal(previous)
	installedJSON, _ := json.Marshal(installed)
	candidateJSON, _ := json.Marshal(candidate)
	if !binding.stageReached(previousJSON) || !binding.forward[0].predecessor(previousJSON) || !binding.forward[0].reversed(previousJSON) || !binding.forward[0].reached(installedJSON) || !binding.forward[1].predecessor(installedJSON) || !binding.forward[1].reached(candidateJSON) || !binding.verifyCandidate.reached(candidateJSON) || !binding.verifyPrevious.reached(previousJSON) {
		t.Fatal("caddy state predicate mismatch")
	}
	if reference, err := binding.rollbackReference(previousJSON); err != nil || reference != testDigest("rollback") {
		t.Fatalf("reference=%q err=%v", reference, err)
	}
}

func TestConcretePostgreSQLBindingUsesExactStatePredicates(t *testing.T) {
	paths := newBindingPaths(t)
	adapter, err := postgresql.Open(postgresql.Options{DockerBinary: "/usr/bin/true", DockerBinaryDigest: binaryDigest(t), StateRoot: paths.directory("state"), DataRoot: paths.directory("data"), SecretRoot: paths.directory("secrets"), ContainerPrefix: "gotth", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	binding, err := BindPostgreSQL("database", adapter, postgresql.Request{ComponentID: "database", Specification: postgresql.Specification{Image: "registry.test/postgresql@" + testDigest("postgresql-image")}, SecretRevisionDigest: testDigest("secret"), ConfigurationDigest: testDigest("configuration")})
	if err != nil || !validBinding(binding) {
		t.Fatalf("binding=%#v err=%v", binding, err)
	}
	states := []postgresql.Observation{
		{Summary: postgresql.Summary{RollbackReference: testDigest("rollback"), HadPrevious: true, PreviousRunning: true}, StageState: postgresql.StateCandidate, PrimaryState: postgresql.StatePrevious, PrimaryPower: postgresql.PowerRunning, RollbackState: postgresql.StateAbsent, RollbackPower: postgresql.PowerAbsent},
		{Summary: postgresql.Summary{HadPrevious: true}, StageState: postgresql.StateCandidate, PrimaryState: postgresql.StatePrevious, PrimaryPower: postgresql.PowerStopped, RollbackState: postgresql.StateAbsent, RollbackPower: postgresql.PowerAbsent},
		{Summary: postgresql.Summary{HadPrevious: true}, StageState: postgresql.StateCandidate, PrimaryState: postgresql.StateAbsent, PrimaryPower: postgresql.PowerAbsent, RollbackState: postgresql.StatePrevious, RollbackPower: postgresql.PowerStopped},
		{Summary: postgresql.Summary{HadPrevious: true}, StageState: postgresql.StateCandidate, PrimaryState: postgresql.StateCandidate, PrimaryPower: postgresql.PowerStopped, RollbackState: postgresql.StatePrevious, RollbackPower: postgresql.PowerStopped},
		{Summary: postgresql.Summary{HadPrevious: true}, StageState: postgresql.StateCandidate, PrimaryState: postgresql.StateCandidate, PrimaryPower: postgresql.PowerRunning, RollbackState: postgresql.StatePrevious, RollbackPower: postgresql.PowerStopped},
	}
	encoded := make([][]byte, len(states))
	for index := range states {
		encoded[index], _ = json.Marshal(states[index])
	}
	for index, transition := range binding.forward {
		if !transition.predecessor(encoded[index]) || !transition.reached(encoded[index+1]) || !transition.reversed(encoded[index]) {
			t.Fatalf("transition %d rejected exact states", index)
		}
	}
	if !binding.verifyCandidate.reached(encoded[4]) || !binding.verifyPrevious.reached(encoded[0]) {
		t.Fatal("postgresql verification predicate mismatch")
	}
}

func TestConcreteAuthentikBindingUsesExactRoleOrder(t *testing.T) {
	paths := newBindingPaths(t)
	adapter, err := authentik.Open(authentik.Options{DockerBinary: "/usr/bin/true", DockerBinaryDigest: binaryDigest(t), StateRoot: paths.directory("state"), DataRoot: paths.directory("data"), SecretRoot: paths.directory("secrets"), ContainerPrefix: "gotth", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	binding, err := BindAuthentik("identity", adapter, authentik.Request{ComponentID: "identity", Specification: authentik.Specification{Image: "registry.test/authentik@" + testDigest("authentik-image")}, ConfigurationDigest: testDigest("configuration"), DatabaseSecretRevisionDigest: testDigest("database"), KeySecretRevisionDigest: testDigest("key")})
	if err != nil || !validBinding(binding) {
		t.Fatalf("binding=%#v err=%v", binding, err)
	}
	previous := authentik.RoleObservation{PrimaryState: authentik.StatePrevious, PrimaryPower: authentik.PowerRunning, RollbackState: authentik.StateAbsent, RollbackPower: authentik.PowerAbsent}
	initial := authentik.Observation{Summary: authentik.Summary{RollbackReference: testDigest("rollback"), HadPrevious: true, PreviousServerRunning: true, PreviousWorkerRunning: true}, StageState: authentik.StateCandidate, Server: previous, Worker: previous}
	initialJSON, _ := json.Marshal(initial)
	if !binding.stageReached(initialJSON) || !binding.forward[0].predecessor(initialJSON) || !binding.forward[0].reversed(initialJSON) || !binding.verifyPrevious.reached(initialJSON) {
		t.Fatal("authentik initial predicate mismatch")
	}
	candidateRole := authentik.RoleObservation{PrimaryState: authentik.StateCandidate, PrimaryPower: authentik.PowerRunning, RollbackState: authentik.StatePrevious, RollbackPower: authentik.PowerStopped}
	candidate := initial
	candidate.Server, candidate.Worker = candidateRole, candidateRole
	candidateJSON, _ := json.Marshal(candidate)
	if !binding.forward[7].reached(candidateJSON) || !binding.verifyCandidate.reached(candidateJSON) {
		t.Fatal("authentik candidate predicate mismatch")
	}
}

func TestAllFiveMailBindingsAreClosedAndRoleSpecific(t *testing.T) {
	paths := newBindingPaths(t)
	runtimeOptions := func(name string) mailruntime.RuntimeOptions {
		return mailruntime.RuntimeOptions{DockerBinary: "/usr/bin/true", DockerBinaryDigest: binaryDigest(t), StateRoot: paths.directory(name + "-state"), ConfigurationRoot: paths.directory(name + "-config"), ContainerPrefix: "gotth", Timeout: time.Second}
	}
	digest := testDigest("secret")
	candidate := func(component string) mailruntime.Candidate {
		return mailruntime.Candidate{ComponentID: component, Image: "registry.test/" + component + "@" + testDigest(component+"-image"), ConfigurationDigest: testDigest(component + "-configuration"), SecretRevisionDigest: digest}
	}
	control, err := mailruntime.OpenControlPlane(mailruntime.ControlPlaneOptions{Runtime: runtimeOptions("control"), DataRoot: paths.directory("control-data"), ExtensionStateRoot: paths.directory("extension-state"), ExtensionSecretRoot: paths.directory("extension-secret"), DatabaseSecretFile: paths.file("database-secret"), MasterSecretFile: paths.file("master-secret"), FrontAuthSecretFile: paths.file("control-front-secret"), OIDCSecretFile: paths.file("oidc-secret"), PostfixHelperFile: paths.file("control-helper-secret"), PostfixReleaseFile: paths.file("control-release-secret")})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	networkBinding, err := BindMailNetwork("mail-network", testDigest("network-artifact"), testDigest("network-configuration"), control)
	if err != nil || !validBinding(networkBinding) {
		t.Fatalf("network binding=%#v err=%v", networkBinding, err)
	}
	absentNetwork, _ := json.Marshal(networkObservation{})
	presentNetwork, _ := json.Marshal(networkObservation{Exists: true, Summary: mailruntime.NetworkSummary{Name: "gotth-mail", ID: "0123456789ab", Digest: testDigest("network")}})
	if !networkBinding.forward[0].predecessor(absentNetwork) || !networkBinding.forward[0].reached(presentNetwork) || networkBinding.forward[0].recoveryOnly != journal.RecoveryNoRollback {
		t.Fatal("network state predicate mismatch")
	}
	front, err := mailruntime.OpenFront(mailruntime.FrontOptions{Runtime: runtimeOptions("front"), CertificateFile: paths.file("certificate"), PrivateKeyFile: paths.file("private-key"), FrontAuthSecretFile: paths.file("front-auth-secret")})
	if err != nil {
		t.Fatal(err)
	}
	defer front.Close()
	postfix, err := mailruntime.OpenPostfix(mailruntime.PostfixOptions{Runtime: runtimeOptions("postfix"), QueueRoot: paths.directory("queue"), HelperSecretFile: paths.file("helper-secret"), ReleaseSecretFile: paths.file("release-secret")})
	if err != nil {
		t.Fatal(err)
	}
	defer postfix.Close()
	dovecot, err := mailruntime.OpenDovecot(mailruntime.DovecotOptions{Runtime: runtimeOptions("dovecot"), MailboxRoot: paths.directory("mailbox"), AuthSecretFile: paths.file("auth-secret")})
	if err != nil {
		t.Fatal(err)
	}
	defer dovecot.Close()
	rspamd, err := mailruntime.OpenRspamd(mailruntime.RspamdOptions{Runtime: runtimeOptions("rspamd"), DataRoot: paths.directory("rspamd-data"), DKIMRoot: paths.directory("dkim"), ControllerSecretFile: paths.file("controller-secret")})
	if err != nil {
		t.Fatal(err)
	}
	defer rspamd.Close()
	bindings := []*Binding{}
	for _, makeBinding := range []func() (*Binding, error){
		func() (*Binding, error) {
			return BindMailControlPlane("control", control, mailruntime.ControlPlaneRequest{Candidate: candidate("control")})
		},
		func() (*Binding, error) {
			return BindMailFront("front", front, mailruntime.FrontRequest{Candidate: candidate("front")})
		},
		func() (*Binding, error) {
			return BindMailPostfix("postfix", postfix, mailruntime.PostfixRequest{Candidate: candidate("postfix")})
		},
		func() (*Binding, error) {
			return BindMailDovecot("dovecot", dovecot, mailruntime.DovecotRequest{Candidate: candidate("dovecot")})
		},
		func() (*Binding, error) {
			return BindMailRspamd("rspamd", rspamd, mailruntime.RspamdRequest{Candidate: candidate("rspamd")})
		},
	} {
		binding, bindErr := makeBinding()
		if bindErr != nil || !validBinding(binding) {
			t.Fatalf("binding=%#v err=%v", binding, bindErr)
		}
		bindings = append(bindings, binding)
	}
	seen := map[string]bool{}
	for _, binding := range bindings {
		if seen[binding.adapterID] {
			t.Fatalf("duplicate adapter %s", binding.adapterID)
		}
		seen[binding.adapterID] = true
	}
	postfixBinding := bindings[2]
	previous := mailruntime.Observation{Summary: mailruntime.Summary{RollbackReference: testDigest("rollback"), HadPrevious: true, PreviousRunning: true}, StageState: mailruntime.StateCandidate, PrimaryState: mailruntime.StatePrevious, PrimaryPower: mailruntime.PowerRunning, RollbackState: mailruntime.StateAbsent, RollbackPower: mailruntime.PowerAbsent}
	previousJSON, _ := json.Marshal(previous)
	if !postfixBinding.stageReached(previousJSON) || !postfixBinding.forward[0].predecessor(previousJSON) || !postfixBinding.verifyPrevious.reached(previousJSON) {
		t.Fatal("mail previous predicate mismatch")
	}
	candidateState := previous
	candidateState.PrimaryState, candidateState.PrimaryPower, candidateState.RollbackState, candidateState.RollbackPower = mailruntime.StateCandidate, mailruntime.PowerRunning, mailruntime.StatePrevious, mailruntime.PowerStopped
	candidateJSON, _ := json.Marshal(candidateState)
	if !postfixBinding.forward[3].reached(candidateJSON) || !postfixBinding.verifyCandidate.reached(candidateJSON) {
		t.Fatal("mail candidate predicate mismatch")
	}
}
