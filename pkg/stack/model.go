// Package stack validates secret-free GOTTH deployment manifests and builds
// deterministic, non-mutating deployment plans.
package stack

import "errors"

const (
	SchemaVersion    = 1
	MaxManifestBytes = 1 << 20
)

var (
	ErrInvalidJSON     = errors.New("manifest JSON is invalid")
	ErrDuplicateName   = errors.New("manifest JSON contains a duplicate object name")
	ErrTooDeep         = errors.New("manifest JSON nesting is too deep")
	ErrManifestLarge   = errors.New("manifest is too large")
	ErrInvalidManifest = errors.New("manifest contract is invalid")
	ErrInvalidPlan     = errors.New("plan contract is invalid")
)

type Artifact struct {
	Source string `json:"source"`
	Digest string `json:"digest"`
}

type Component struct {
	ID                  string   `json:"id"`
	Adapter             string   `json:"adapter"`
	Artifact            Artifact `json:"artifact"`
	ConfigurationDigest string   `json:"configuration_digest"`
	DependsOn           []string `json:"depends_on"`
	Capabilities        []string `json:"capabilities"`
	SecretSlots         []string `json:"secret_slots"`
}

type Manifest struct {
	SchemaVersion int         `json:"schema_version"`
	Name          string      `json:"name"`
	Components    []Component `json:"components"`
}

type PlanStep struct {
	Index               int      `json:"index"`
	ComponentID         string   `json:"component_id"`
	Adapter             string   `json:"adapter"`
	Artifact            Artifact `json:"artifact"`
	ConfigurationDigest string   `json:"configuration_digest"`
	DependsOn           []string `json:"depends_on"`
	Capabilities        []string `json:"capabilities"`
	SecretSlots         []string `json:"secret_slots"`
}

type Plan struct {
	SchemaVersion  int        `json:"schema_version"`
	Stack          string     `json:"stack"`
	ManifestDigest string     `json:"manifest_digest"`
	Steps          []PlanStep `json:"steps"`
	Digest         string     `json:"digest"`
}
