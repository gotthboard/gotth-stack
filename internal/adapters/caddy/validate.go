package caddy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,78}[a-z0-9])?$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func validID(value string) bool {
	return len(value) <= 80 && idPattern.MatchString(value)
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validateConfiguration(value []byte) error {
	if len(value) == 0 {
		return ErrInvalidInput
	}
	if len(value) > MaxConfigBytes {
		return ErrLimit
	}
	if !utf8.Valid(value) || bytes.IndexByte(value, 0) >= 0 {
		return ErrInvalidInput
	}
	clean := stripComments(string(value))
	if containsBareToken(clean, "import") {
		return ErrInvalidInput
	}
	if strings.Contains(clean, "{$") || strings.Contains(clean, "{env.") {
		return ErrInvalidInput
	}
	return nil
}

func containsBareToken(value, wanted string) bool {
	var token strings.Builder
	var quote rune
	escaped := false
	flush := func() bool {
		matched := token.String() == wanted
		token.Reset()
		return matched
	}
	for _, current := range value {
		if quote != 0 {
			if quote == '"' && current == '\\' && !escaped {
				escaped = true
				continue
			}
			if current == quote && !escaped {
				quote = 0
			}
			escaped = false
			continue
		}
		if current == '"' || current == '`' {
			if flush() {
				return true
			}
			quote = current
			continue
		}
		if current == ' ' || current == '\t' || current == '\r' || current == '\n' || current == '{' || current == '}' {
			if flush() {
				return true
			}
			continue
		}
		token.WriteRune(current)
	}
	return flush()
}

func stripComments(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	var quote rune
	escaped := false
	inComment := false
	for _, current := range value {
		if current == '\n' {
			if quote != '`' {
				quote = 0
			}
			escaped = false
			inComment = false
			result.WriteRune(current)
			continue
		}
		if inComment {
			continue
		}
		if quote != 0 {
			result.WriteRune(current)
			if quote == '"' && current == '\\' && !escaped {
				escaped = true
				continue
			}
			if current == quote && !escaped {
				quote = 0
			}
			escaped = false
			continue
		}
		if current == '#' {
			inComment = true
			continue
		}
		if current == '"' || current == '`' {
			quote = current
		}
		result.WriteRune(current)
	}
	return result.String()
}

func canonicalJSON(value []byte) ([]byte, error) {
	if len(value) == 0 {
		return nil, ErrRuntime
	}
	if len(value) > MaxJSONBytes {
		return nil, ErrLimit
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, ErrRuntime
	}
	if _, ok := decoded.(map[string]any); !ok {
		return nil, ErrRuntime
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, ErrRuntime
	}
	encoded, err := json.Marshal(decoded)
	if err != nil || len(encoded) > MaxJSONBytes {
		return nil, ErrRuntime
	}
	return encoded, nil
}

func adminBound(value []byte, address string) bool {
	var document struct {
		Admin *struct {
			Listen   string `json:"listen"`
			Disabled bool   `json:"disabled"`
		} `json:"admin"`
	}
	if err := json.Unmarshal(value, &document); err != nil || document.Admin == nil {
		return false
	}
	return !document.Admin.Disabled && document.Admin.Listen == address
}

func summary(metadata transactionMetadata) Summary {
	return Summary{
		SchemaVersion: SchemaVersion, OperationID: metadata.OperationID,
		ComponentID: metadata.ComponentID, BindingDigest: metadata.BindingDigest, RollbackReference: metadata.RollbackReference,
		PreviousFileDigest: metadata.PreviousFileDigest, CandidateFileDigest: metadata.CandidateFileDigest,
		PreviousRuntimeDigest: metadata.PreviousRuntimeDigest, CandidateRuntimeDigest: metadata.CandidateRuntimeDigest,
	}
}
