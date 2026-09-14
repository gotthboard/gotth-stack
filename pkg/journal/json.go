package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const maxJSONDepth = 32

// decodeStrict rejects duplicate names and unknown fields. Standard JSON
// decoding accepts duplicate object members, which is unacceptable for an
// authority record because two readers could assign different meaning.
func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := inspectJSONValue(decoder, 0); err != nil {
		return ErrCorrupt
	}
	if err := requireJSONEnd(decoder); err != nil {
		return ErrCorrupt
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrCorrupt
	}
	if err := requireJSONEnd(decoder); err != nil {
		return ErrCorrupt
	}
	return nil
}

func decodeCanonicalDocument(raw []byte, target any) error {
	if err := decodeStrict(raw, target); err != nil {
		return err
	}
	canonical, err := json.Marshal(target)
	if err != nil {
		return ErrCorrupt
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(raw, canonical) {
		return ErrCorrupt
	}
	return nil
}

func inspectJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	if (delimiter != '{' && delimiter != '[') || depth >= maxJSONDepth {
		return ErrCorrupt
	}
	if delimiter == '{' {
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := nameToken.(string)
			if !ok {
				return ErrCorrupt
			}
			if _, exists := seen[name]; exists {
				return ErrCorrupt
			}
			seen[name] = struct{}{}
			if err := inspectJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	} else {
		for decoder.More() {
			if err := inspectJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	end, ok := closing.(json.Delim)
	if !ok || (delimiter == '{' && end != '}') || (delimiter == '[' && end != ']') {
		return ErrCorrupt
	}
	return nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	if _, err := decoder.Token(); errors.Is(err, io.EOF) {
		return nil
	}
	return ErrCorrupt
}
