package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type strictResponseKey struct{}

func WithStrictResponse(ctx context.Context) context.Context {
	return context.WithValue(ctx, strictResponseKey{}, true)
}
func decodeResponse(ctx context.Context, text string, result any) error {
	if strict, _ := ctx.Value(strictResponseKey{}).(bool); !strict {
		return ExtractJSONObject(text, result)
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate or invalid response object key")
				}
				seen[name] = true
				if err = walk(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err = walk(); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("response must contain exactly one complete JSON object")
	}
	return decodeStrictJSON(strings.TrimSpace(text), result)
}
