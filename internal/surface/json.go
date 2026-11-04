package surface

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// parseOutput rejects ambiguous data before encoding/json can replace invalid
// strings or overwrite duplicate keys. Numbers retain their original spelling.
func parseOutput(data []byte) (any, error) {
	if len(data) == 0 || len(data) > MaxOutputBytes || !utf8.Valid(data) || !json.Valid(data) {
		return nil, errors.New("output must be one UTF-8 JSON value of at most 1 MiB")
	}
	if err := validSurrogates(data); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return readValue(d, 0)
}

func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, errors.New("JSON nesting exceeds 64")
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		obj := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("object key is not a string")
			}
			if _, exists := obj[name]; exists {
				return nil, fmt.Errorf("duplicate key %q", name)
			}
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			obj[name] = value
		}
		_, err = d.Token()
		return obj, err
	case json.Delim('['):
		array := []any{}
		for d.More() {
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		_, err = d.Token()
		return array, err
	default:
		return token, nil
	}
}

// json.Valid establishes escape lengths and hex syntax before this scan.
// A high surrogate must be followed immediately by a low surrogate; isolated
// halves would otherwise collapse to the same replacement character in Go.
func validSurrogates(data []byte) error {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		n, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return err
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return errors.New("isolated low surrogate")
		}
		if n < 0xd800 || n > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return errors.New("isolated high surrogate")
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return errors.New("invalid surrogate pair")
		}
		i += 6
	}
	return nil
}
