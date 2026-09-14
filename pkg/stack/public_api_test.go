package stack_test

import (
	"strings"
	"testing"

	"github.com/gotthboard/gotth-stack/pkg/stack"
)

func TestExternalConsumerCanBuildAndMarshalPlan(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	manifest := stack.Manifest{
		SchemaVersion: stack.SchemaVersion,
		Name:          "consumer-stack",
		Components: []stack.Component{{
			ID:                  "proxy",
			Adapter:             "gotth-stack-adapter-caddy.v1",
			Artifact:            stack.Artifact{Source: "oci://registry.example.test/caddy", Digest: digest},
			ConfigurationDigest: digest,
		}},
	}
	plan, err := stack.BuildPlan(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stack.MarshalPlan(plan); err != nil {
		t.Fatal(err)
	}
}
