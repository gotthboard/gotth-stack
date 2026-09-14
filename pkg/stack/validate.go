package stack

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxComponents = 64
	maxSetValues  = 64
)

var (
	slugPattern       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	adapterPattern    = regexp.MustCompile(`^gotth-stack-adapter-[a-z0-9]+(?:-[a-z0-9]+)*\.v[1-9][0-9]*$`)
	capabilityPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:\.[a-z0-9]+(?:-[a-z0-9]+)*)*$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != SchemaVersion || !validSlug(manifest.Name) {
		return ErrInvalidManifest
	}
	if len(manifest.Components) < 1 || len(manifest.Components) > maxComponents {
		return ErrInvalidManifest
	}

	components := make(map[string]struct{}, len(manifest.Components))
	for _, component := range manifest.Components {
		if err := validateComponent(component); err != nil {
			return err
		}
		if _, exists := components[component.ID]; exists {
			return ErrInvalidManifest
		}
		components[component.ID] = struct{}{}
	}
	for _, component := range manifest.Components {
		for _, dependency := range component.DependsOn {
			if dependency == component.ID {
				return ErrInvalidManifest
			}
			if _, exists := components[dependency]; !exists {
				return ErrInvalidManifest
			}
		}
	}
	if _, err := dependencyOrder(manifest.Components); err != nil {
		return err
	}
	return nil
}

func validateComponent(component Component) error {
	if !validSlug(component.ID) || len(component.Adapter) > 160 || !adapterPattern.MatchString(component.Adapter) {
		return ErrInvalidManifest
	}
	if !validArtifactSource(component.Artifact.Source) || !digestPattern.MatchString(component.Artifact.Digest) || !digestPattern.MatchString(component.ConfigurationDigest) {
		return ErrInvalidManifest
	}
	if err := validateUnique(component.DependsOn, validSlug); err != nil {
		return err
	}
	if err := validateUnique(component.Capabilities, validCapability); err != nil {
		return err
	}
	return validateUnique(component.SecretSlots, validSlug)
}

func validSlug(value string) bool {
	return len(value) <= 80 && slugPattern.MatchString(value)
}

func validCapability(value string) bool {
	return len(value) <= 160 && capabilityPattern.MatchString(value)
}

func validArtifactSource(source string) bool {
	if len(source) < 1 || len(source) > 2048 || !utf8.ValidString(source) || strings.TrimSpace(source) != source || strings.IndexFunc(source, unicode.IsControl) >= 0 {
		return false
	}
	parsed, err := url.Parse(source)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "oci") || parsed.Host == "" || parsed.Path == "" {
		return false
	}
	return parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && !strings.Contains(parsed.Path, "\\") && strings.IndexFunc(parsed.Path, unicode.IsControl) < 0
}

func validateUnique(values []string, validate func(string) bool) error {
	if len(values) > maxSetValues {
		return ErrInvalidManifest
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validate(value) {
			return ErrInvalidManifest
		}
		if _, exists := seen[value]; exists {
			return ErrInvalidManifest
		}
		seen[value] = struct{}{}
	}
	return nil
}

func dependencyOrder(components []Component) ([]string, error) {
	indegree := make(map[string]int, len(components))
	children := make(map[string][]string, len(components))
	for _, component := range components {
		indegree[component.ID] = len(component.DependsOn)
		for _, dependency := range component.DependsOn {
			children[dependency] = append(children[dependency], component.ID)
		}
	}

	ready := make([]string, 0, len(components))
	for id, count := range indegree {
		if count == 0 {
			ready = insertSorted(ready, id)
		}
	}
	order := make([]string, 0, len(components))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		for _, child := range children[id] {
			indegree[child]--
			if indegree[child] == 0 {
				ready = insertSorted(ready, child)
			}
		}
	}
	if len(order) != len(components) {
		return nil, errors.Join(ErrInvalidManifest, errors.New("dependency graph is cyclic"))
	}
	return order, nil
}

func insertSorted(values []string, value string) []string {
	index := 0
	for index < len(values) && values[index] < value {
		index++
	}
	values = append(values, "")
	copy(values[index+1:], values[index:])
	values[index] = value
	return values
}
