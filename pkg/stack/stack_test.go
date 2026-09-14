package stack

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed with attacker text") }

func TestParseManifestAcceptsStrictContract(t *testing.T) {
	encoded, err := json.Marshal(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manifest, validManifest()) {
		t.Fatalf("manifest mismatch: %#v", manifest)
	}
}

func TestParseManifestRejectsAmbiguousAndUnboundedJSON(t *testing.T) {
	valid, err := json.Marshal(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{name: "empty", raw: "", want: ErrInvalidJSON},
		{name: "malformed", raw: "{", want: ErrInvalidJSON},
		{name: "unknown", raw: strings.Replace(string(valid), `"name":"test-stack"`, `"name":"test-stack","unknown":true`, 1), want: ErrInvalidJSON},
		{name: "duplicate root", raw: strings.Replace(string(valid), `"name":"test-stack"`, `"name":"test-stack","name":"other"`, 1), want: ErrDuplicateName},
		{name: "duplicate nested", raw: strings.Replace(string(valid), `"source":"oci://registry.example.test/caddy"`, `"source":"oci://registry.example.test/caddy","source":"https://other.example.test/x"`, 1), want: ErrDuplicateName},
		{name: "trailing", raw: string(valid) + " true", want: ErrInvalidJSON},
		{name: "too deep", raw: strings.Repeat("[", maxJSONDepth+1) + "0" + strings.Repeat("]", maxJSONDepth+1), want: ErrTooDeep},
		{name: "unexpected close", raw: "]", want: ErrInvalidJSON},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseManifest(strings.NewReader(test.raw))
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
	if _, err := ParseManifest(nil); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("nil reader: %v", err)
	}
	oversized := strings.Repeat(" ", MaxManifestBytes+1)
	if _, err := ParseManifest(strings.NewReader(oversized)); !errors.Is(err, ErrManifestLarge) {
		t.Fatalf("oversized input: %v", err)
	}
}

func TestParseManifestRejectsReadAndSemanticFailuresWithoutDisclosure(t *testing.T) {
	if _, err := ParseManifest(errorReader{}); !errors.Is(err, ErrInvalidJSON) || strings.Contains(err.Error(), "attacker") {
		t.Fatalf("read failure was not safely mapped: %v", err)
	}
	invalid := validManifest()
	invalid.SchemaVersion = 99
	encoded, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseManifest(bytes.NewReader(encoded)); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("semantic failure was not rejected: %v", err)
	}
}

func TestManifestValidationRejectsEveryContractClass(t *testing.T) {
	tooManyComponents := make([]Component, maxComponents+1)
	for index := range tooManyComponents {
		tooManyComponents[index] = component("component-" + base26(index))
	}
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{name: "schema", mutate: func(value *Manifest) { value.SchemaVersion = 2 }},
		{name: "stack name", mutate: func(value *Manifest) { value.Name = "Bad Name" }},
		{name: "stack name too long", mutate: func(value *Manifest) { value.Name = strings.Repeat("a", 81) }},
		{name: "empty components", mutate: func(value *Manifest) { value.Components = nil }},
		{name: "too many components", mutate: func(value *Manifest) { value.Components = tooManyComponents }},
		{name: "component id", mutate: func(value *Manifest) { value.Components[0].ID = "Bad" }},
		{name: "component id too long", mutate: func(value *Manifest) { value.Components[0].ID = strings.Repeat("a", 81) }},
		{name: "duplicate component", mutate: func(value *Manifest) { value.Components[1].ID = value.Components[0].ID }},
		{name: "adapter", mutate: func(value *Manifest) { value.Components[0].Adapter = "shell.v1" }},
		{name: "adapter zero", mutate: func(value *Manifest) { value.Components[0].Adapter = "gotth-stack-adapter-caddy.v0" }},
		{name: "adapter too long", mutate: func(value *Manifest) {
			value.Components[0].Adapter = "gotth-stack-adapter-" + strings.Repeat("a", 150) + ".v1"
		}},
		{name: "artifact source scheme", mutate: func(value *Manifest) { value.Components[0].Artifact.Source = "http://registry.example.test/caddy" }},
		{name: "artifact source too long", mutate: func(value *Manifest) {
			value.Components[0].Artifact.Source = "https://example.test/" + strings.Repeat("a", 2049)
		}},
		{name: "artifact source credentials", mutate: func(value *Manifest) {
			value.Components[0].Artifact.Source = "https://user:pass@registry.example.test/caddy"
		}},
		{name: "artifact source query", mutate: func(value *Manifest) {
			value.Components[0].Artifact.Source = "https://registry.example.test/caddy?token=x"
		}},
		{name: "artifact source fragment", mutate: func(value *Manifest) { value.Components[0].Artifact.Source = "https://registry.example.test/caddy#x" }},
		{name: "artifact source no path", mutate: func(value *Manifest) { value.Components[0].Artifact.Source = "https://registry.example.test" }},
		{name: "artifact digest", mutate: func(value *Manifest) { value.Components[0].Artifact.Digest = "sha256:ABC" }},
		{name: "configuration digest", mutate: func(value *Manifest) { value.Components[0].ConfigurationDigest = "" }},
		{name: "missing dependency", mutate: func(value *Manifest) { value.Components[0].DependsOn = []string{"missing"} }},
		{name: "self dependency", mutate: func(value *Manifest) { value.Components[0].DependsOn = []string{value.Components[0].ID} }},
		{name: "duplicate dependency", mutate: func(value *Manifest) { value.Components[1].DependsOn = []string{"caddy", "caddy"} }},
		{name: "bad capability", mutate: func(value *Manifest) { value.Components[0].Capabilities = []string{"Host.Root"} }},
		{name: "capability too long", mutate: func(value *Manifest) { value.Components[0].Capabilities = []string{strings.Repeat("a", 161)} }},
		{name: "duplicate capability", mutate: func(value *Manifest) { value.Components[0].Capabilities = []string{"network.listen", "network.listen"} }},
		{name: "bad secret slot", mutate: func(value *Manifest) { value.Components[0].SecretSlots = []string{"DB_PASSWORD"} }},
		{name: "secret slot too long", mutate: func(value *Manifest) { value.Components[0].SecretSlots = []string{strings.Repeat("a", 81)} }},
		{name: "duplicate secret slot", mutate: func(value *Manifest) { value.Components[0].SecretSlots = []string{"tls-key", "tls-key"} }},
		{name: "cycle", mutate: func(value *Manifest) { value.Components[0].DependsOn = []string{"mail"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validManifest()
			test.mutate(&manifest)
			if _, err := BuildPlan(manifest); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestManifestValidationRejectsOversizedSets(t *testing.T) {
	for _, field := range []string{"dependencies", "capabilities", "secrets"} {
		t.Run(field, func(t *testing.T) {
			manifest := validManifest()
			values := make([]string, maxSetValues+1)
			for index := range values {
				values[index] = "value-" + base26(index)
			}
			switch field {
			case "dependencies":
				manifest.Components[0].DependsOn = values
			case "capabilities":
				manifest.Components[0].Capabilities = values
			case "secrets":
				manifest.Components[0].SecretSlots = values
			}
			if _, err := BuildPlan(manifest); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestBuildPlanIsDeterministicAndDoesNotMutateInput(t *testing.T) {
	first := validManifest()
	original := cloneManifestForTest(first)
	second := validManifest()
	slices.Reverse(second.Components)
	for index := range second.Components {
		slices.Reverse(second.Components[index].Capabilities)
		slices.Reverse(second.Components[index].SecretSlots)
		slices.Reverse(second.Components[index].DependsOn)
	}

	firstPlan, err := BuildPlan(first)
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, err := BuildPlan(second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, original) {
		t.Fatal("BuildPlan mutated its input")
	}
	if !reflect.DeepEqual(firstPlan, secondPlan) {
		t.Fatalf("plans differ:\n%#v\n%#v", firstPlan, secondPlan)
	}
	gotOrder := []string{firstPlan.Steps[0].ComponentID, firstPlan.Steps[1].ComponentID}
	if !reflect.DeepEqual(gotOrder, []string{"caddy", "mail"}) {
		t.Fatalf("unexpected order: %v", gotOrder)
	}
	if !digestPattern.MatchString(firstPlan.ManifestDigest) || !digestPattern.MatchString(firstPlan.Digest) {
		t.Fatalf("invalid digests: %#v", firstPlan)
	}
	encoded, err := MarshalPlan(firstPlan)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(encoded, []byte("\n")) || bytes.Contains(encoded, []byte("secret-value")) || bytes.Contains(encoded, []byte(": null")) {
		t.Fatalf("bad encoding: %q", encoded)
	}
	encodedAgain, err := MarshalPlan(secondPlan)
	if err != nil || !bytes.Equal(encoded, encodedAgain) {
		t.Fatalf("encoding not deterministic: %v", err)
	}
}

func TestMarshalPlanRejectsTampering(t *testing.T) {
	base, err := BuildPlan(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Plan)
	}{
		{name: "schema", mutate: func(value *Plan) { value.SchemaVersion = 2 }},
		{name: "stack", mutate: func(value *Plan) { value.Stack = "Bad" }},
		{name: "manifest digest", mutate: func(value *Plan) { value.ManifestDigest = "" }},
		{name: "valid but inconsistent manifest digest", mutate: func(value *Plan) {
			value.ManifestDigest = testDigest('f')
			value.Digest = planDigestForTest(*value)
		}},
		{name: "digest", mutate: func(value *Plan) { value.Digest = strings.Repeat("0", 71) }},
		{name: "empty", mutate: func(value *Plan) { value.Steps = nil }},
		{name: "null dependency set", mutate: func(value *Plan) {
			value.Steps[0].DependsOn = nil
			value.Digest = planDigestForTest(*value)
		}},
		{name: "null capability set", mutate: func(value *Plan) {
			value.Steps[0].Capabilities = nil
			value.Digest = planDigestForTest(*value)
		}},
		{name: "null secret set", mutate: func(value *Plan) {
			value.Steps[0].SecretSlots = nil
			value.Digest = planDigestForTest(*value)
		}},
		{name: "index", mutate: func(value *Plan) { value.Steps[0].Index = 2 }},
		{name: "component", mutate: func(value *Plan) { value.Steps[0].Adapter = "shell" }},
		{name: "duplicate", mutate: func(value *Plan) { value.Steps[1].ComponentID = value.Steps[0].ComponentID }},
		{name: "dependency order", mutate: func(value *Plan) { value.Steps[0].DependsOn = []string{"mail"} }},
		{name: "unordered set", mutate: func(value *Plan) { value.Steps[1].Capabilities = []string{"service.manage", "database.migrate"} }},
		{name: "noncanonical independent order", mutate: func(value *Plan) {
			value.Steps[1].DependsOn = nil
			value.Steps[0], value.Steps[1] = value.Steps[1], value.Steps[0]
			value.Steps[0].Index = 1
			value.Steps[1].Index = 2
		}},
		{name: "content", mutate: func(value *Plan) { value.Steps[0].Artifact.Source = "https://registry.example.test/changed" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := clonePlanForTest(base)
			test.mutate(&plan)
			if _, err := MarshalPlan(plan); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestArtifactSourceAndCapabilityBoundaries(t *testing.T) {
	validSources := []string{
		"https://downloads.example.test/releases/gotth-mail",
		"oci://registry.example.test/gotth/mail",
	}
	for _, source := range validSources {
		if !validArtifactSource(source) {
			t.Fatalf("valid source rejected: %q", source)
		}
	}
	invalidSources := []string{"", " https://example.test/x", "file:///tmp/x", "oci://registry.example.test", "oci://registry.example.test/x\\y", "https://example.test/%0a", "https://example.test/\x00", "https://example.test/\xff"}
	for _, source := range invalidSources {
		if validArtifactSource(source) {
			t.Fatalf("invalid source accepted: %q", source)
		}
	}
	for _, capability := range []string{"network.listen", "database.migrate", "service-health.read"} {
		if !validCapability(capability) {
			t.Fatalf("valid capability rejected: %q", capability)
		}
	}
}

func TestInsertSortedHandlesBeginningMiddleAndEnd(t *testing.T) {
	values := []string{"b", "d"}
	values = insertSorted(values, "a")
	values = insertSorted(values, "c")
	values = insertSorted(values, "e")
	if !reflect.DeepEqual(values, []string{"a", "b", "c", "d", "e"}) {
		t.Fatalf("unexpected values: %v", values)
	}
}

func FuzzParseManifest(f *testing.F) {
	valid, _ := json.Marshal(validManifest())
	f.Add(valid)
	f.Add([]byte(`{"schema_version":1,"name":"x","name":"y"}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = ParseManifest(bytes.NewReader(raw))
	})
}

func FuzzBuildPlan(f *testing.F) {
	f.Add("stack", "component", "adapter", "capability")
	f.Fuzz(func(t *testing.T, stackName, componentID, adapterSlug, capability string) {
		manifest := Manifest{
			SchemaVersion: SchemaVersion,
			Name:          stackName,
			Components: []Component{{
				ID:      componentID,
				Adapter: "gotth-stack-adapter-" + adapterSlug + ".v1",
				Artifact: Artifact{
					Source: "https://artifacts.example.test/component",
					Digest: testDigest('a'),
				},
				ConfigurationDigest: testDigest('b'),
				Capabilities:        []string{capability},
			}},
		}
		_, _ = BuildPlan(manifest)
	})
}

func validManifest() Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		Name:          "test-stack",
		Components: []Component{
			{
				ID:      "caddy",
				Adapter: "gotth-stack-adapter-caddy.v1",
				Artifact: Artifact{
					Source: "oci://registry.example.test/caddy",
					Digest: testDigest('a'),
				},
				ConfigurationDigest: testDigest('b'),
				Capabilities:        []string{"network.listen", "tls.manage"},
				SecretSlots:         []string{"tls-key"},
			},
			{
				ID:      "mail",
				Adapter: "gotth-stack-adapter-gotth-mail.v1",
				Artifact: Artifact{
					Source: "https://downloads.example.test/gotth-mail/release",
					Digest: testDigest('c'),
				},
				ConfigurationDigest: testDigest('d'),
				DependsOn:           []string{"caddy"},
				Capabilities:        []string{"database.migrate", "service.manage"},
				SecretSlots:         []string{"database-url", "oidc-secret"},
			},
		},
	}
}

func component(id string) Component {
	return Component{
		ID:                  id,
		Adapter:             "gotth-stack-adapter-test.v1",
		Artifact:            Artifact{Source: "https://artifacts.example.test/component", Digest: testDigest('a')},
		ConfigurationDigest: testDigest('b'),
	}
}

func testDigest(value byte) string {
	return "sha256:" + strings.Repeat(string(value), 64)
}

func base26(value int) string {
	first := byte('a' + value/26)
	second := byte('a' + value%26)
	return string([]byte{first, second})
}

func cloneManifestForTest(value Manifest) Manifest {
	encoded, _ := json.Marshal(value)
	var result Manifest
	_ = json.Unmarshal(encoded, &result)
	return result
}

func clonePlanForTest(value Plan) Plan {
	encoded, _ := json.Marshal(value)
	var result Plan
	_ = json.Unmarshal(encoded, &result)
	return result
}

func planDigestForTest(value Plan) string {
	value.Digest = ""
	encoded, _ := json.Marshal(value)
	return digest(encoded)
}
