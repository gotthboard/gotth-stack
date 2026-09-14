package stack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func BuildPlan(manifest Manifest) (Plan, error) {
	if err := validateManifest(manifest); err != nil {
		return Plan{}, err
	}
	normalized := normalizeManifest(manifest)
	manifestJSON, err := json.Marshal(normalized)
	if err != nil {
		return Plan{}, ErrInvalidManifest
	}
	order, err := dependencyOrder(normalized.Components)
	if err != nil {
		return Plan{}, err
	}
	byID := make(map[string]Component, len(normalized.Components))
	for _, component := range normalized.Components {
		byID[component.ID] = component
	}

	plan := Plan{
		SchemaVersion:  SchemaVersion,
		Stack:          normalized.Name,
		ManifestDigest: digest(manifestJSON),
		Steps:          make([]PlanStep, 0, len(order)),
	}
	for index, id := range order {
		component := byID[id]
		plan.Steps = append(plan.Steps, PlanStep{
			Index:               index + 1,
			ComponentID:         component.ID,
			Adapter:             component.Adapter,
			Artifact:            component.Artifact,
			ConfigurationDigest: component.ConfigurationDigest,
			DependsOn:           append([]string{}, component.DependsOn...),
			Capabilities:        append([]string{}, component.Capabilities...),
			SecretSlots:         append([]string{}, component.SecretSlots...),
		})
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return Plan{}, ErrInvalidPlan
	}
	plan.Digest = digest(planJSON)
	return plan, nil
}

func MarshalPlan(plan Plan) ([]byte, error) {
	if err := validatePlan(plan); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, ErrInvalidPlan
	}
	return append(encoded, '\n'), nil
}

func validatePlan(plan Plan) error {
	if plan.SchemaVersion != SchemaVersion || !validSlug(plan.Stack) || !digestPattern.MatchString(plan.ManifestDigest) || !digestPattern.MatchString(plan.Digest) {
		return ErrInvalidPlan
	}
	if len(plan.Steps) < 1 || len(plan.Steps) > maxComponents {
		return ErrInvalidPlan
	}
	seen := make(map[string]struct{}, len(plan.Steps))
	positions := make(map[string]int, len(plan.Steps))
	components := make([]Component, 0, len(plan.Steps))
	for index, step := range plan.Steps {
		if step.Index != index+1 {
			return ErrInvalidPlan
		}
		if step.DependsOn == nil || step.Capabilities == nil || step.SecretSlots == nil {
			return ErrInvalidPlan
		}
		if !sort.StringsAreSorted(step.DependsOn) || !sort.StringsAreSorted(step.Capabilities) || !sort.StringsAreSorted(step.SecretSlots) {
			return ErrInvalidPlan
		}
		component := Component{
			ID:                  step.ComponentID,
			Adapter:             step.Adapter,
			Artifact:            step.Artifact,
			ConfigurationDigest: step.ConfigurationDigest,
			DependsOn:           step.DependsOn,
			Capabilities:        step.Capabilities,
			SecretSlots:         step.SecretSlots,
		}
		if err := validateComponent(component); err != nil {
			return ErrInvalidPlan
		}
		if _, exists := seen[step.ComponentID]; exists {
			return ErrInvalidPlan
		}
		seen[step.ComponentID] = struct{}{}
		positions[step.ComponentID] = index
		components = append(components, component)
	}
	for index, step := range plan.Steps {
		for _, dependency := range step.DependsOn {
			position, exists := positions[dependency]
			if !exists || position >= index {
				return ErrInvalidPlan
			}
		}
	}
	expectedOrder, err := dependencyOrder(components)
	if err != nil {
		return ErrInvalidPlan
	}
	for index, id := range expectedOrder {
		if plan.Steps[index].ComponentID != id {
			return ErrInvalidPlan
		}
	}
	normalizedManifest := normalizeManifest(Manifest{
		SchemaVersion: plan.SchemaVersion,
		Name:          plan.Stack,
		Components:    components,
	})
	manifestJSON, err := json.Marshal(normalizedManifest)
	if err != nil || digest(manifestJSON) != plan.ManifestDigest {
		return ErrInvalidPlan
	}
	unsigned := plan
	unsigned.Digest = ""
	encoded, err := json.Marshal(unsigned)
	if err != nil || digest(encoded) != plan.Digest {
		return ErrInvalidPlan
	}
	return nil
}

func normalizeManifest(manifest Manifest) Manifest {
	normalized := Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Name:          manifest.Name,
		Components:    make([]Component, len(manifest.Components)),
	}
	for index, component := range manifest.Components {
		normalized.Components[index] = Component{
			ID:                  component.ID,
			Adapter:             component.Adapter,
			Artifact:            component.Artifact,
			ConfigurationDigest: component.ConfigurationDigest,
			DependsOn:           sortedCopy(component.DependsOn),
			Capabilities:        sortedCopy(component.Capabilities),
			SecretSlots:         sortedCopy(component.SecretSlots),
		}
	}
	sort.Slice(normalized.Components, func(left, right int) bool {
		return normalized.Components[left].ID < normalized.Components[right].ID
	})
	return normalized
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if result == nil {
		return []string{}
	}
	return result
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
