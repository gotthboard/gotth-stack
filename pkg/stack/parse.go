package stack

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const maxJSONDepth = 32

func ParseManifest(reader io.Reader) (Manifest, error) {
	if reader == nil {
		return Manifest{}, ErrInvalidJSON
	}
	raw, err := io.ReadAll(io.LimitReader(reader, MaxManifestBytes+1))
	if err != nil {
		return Manifest{}, ErrInvalidJSON
	}
	if len(raw) > MaxManifestBytes {
		return Manifest{}, ErrManifestLarge
	}
	if err := inspectJSON(raw); err != nil {
		return Manifest{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, ErrInvalidJSON
	}
	if err := requireJSONEnd(decoder); err != nil {
		return Manifest{}, err
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func inspectJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := inspectJSONValue(decoder, 0); err != nil {
		return err
	}
	return requireJSONEnd(decoder)
}

func inspectJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidJSON
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return ErrInvalidJSON
	}
	if depth >= maxJSONDepth {
		return ErrTooDeep
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return ErrInvalidJSON
			}
			name, ok := nameToken.(string)
			if !ok {
				return ErrInvalidJSON
			}
			if _, exists := seen[name]; exists {
				return ErrDuplicateName
			}
			seen[name] = struct{}{}
			if err := inspectJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := inspectJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	}

	closing, err := decoder.Token()
	if err != nil {
		return ErrInvalidJSON
	}
	closingDelimiter, ok := closing.(json.Delim)
	if !ok || (delimiter == '{' && closingDelimiter != '}') || (delimiter == '[' && closingDelimiter != ']') {
		return ErrInvalidJSON
	}
	return nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	if _, err := decoder.Token(); errors.Is(err, io.EOF) {
		return nil
	}
	return ErrInvalidJSON
}
