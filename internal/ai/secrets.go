package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/zalando/go-keyring"
)

const keyringService = "ai-devops-devcycle"

var (
	secretIDPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,127}$`)
	environmentPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{1,127}$`)
)

type SecretStore interface {
	Set(context.Context, string, string) (string, error)
	Get(context.Context, string) (string, error)
	Delete(context.Context, string) error
}

// KeyringSecretStore uses the operating system credential store. Environment
// references are resolved without copying their values into DevCycle storage.
type KeyringSecretStore struct{}

func NewKeyringSecretStore() *KeyringSecretStore { return &KeyringSecretStore{} }

func (s *KeyringSecretStore) Set(_ context.Context, id, value string) (string, error) {
	id = strings.TrimSpace(id)
	if !secretIDPattern.MatchString(id) {
		return "", errors.New("credential ID contains unsafe characters")
	}
	if strings.TrimSpace(value) == "" {
		return "", errors.New("credential value is empty")
	}
	if err := keyring.Set(keyringService, id, value); err != nil {
		return "", fmt.Errorf("store credential in the operating system keyring: %w", err)
	}
	return "keyring:" + id, nil
}

func (s *KeyringSecretStore) Get(_ context.Context, ref string) (string, error) {
	switch {
	case strings.HasPrefix(ref, "env:"):
		name := strings.TrimPrefix(ref, "env:")
		if !environmentPattern.MatchString(name) {
			return "", errors.New("credential environment reference is invalid")
		}
		value, ok := os.LookupEnv(name)
		if !ok || value == "" {
			return "", fmt.Errorf("credential environment variable %s is not set", name)
		}
		return value, nil
	case strings.HasPrefix(ref, "keyring:"):
		id := strings.TrimPrefix(ref, "keyring:")
		if !secretIDPattern.MatchString(id) {
			return "", errors.New("credential keyring reference is invalid")
		}
		value, err := keyring.Get(keyringService, id)
		if err != nil {
			return "", fmt.Errorf("read credential from the operating system keyring: %w", err)
		}
		return value, nil
	case ref == "":
		return "", errors.New("credential is not configured")
	default:
		return "", errors.New("unsupported credential reference")
	}
}

func (s *KeyringSecretStore) Delete(_ context.Context, ref string) error {
	if ref == "" || strings.HasPrefix(ref, "env:") {
		return nil
	}
	if !strings.HasPrefix(ref, "keyring:") {
		return errors.New("unsupported credential reference")
	}
	id := strings.TrimPrefix(ref, "keyring:")
	if !secretIDPattern.MatchString(id) {
		return errors.New("credential keyring reference is invalid")
	}
	if err := keyring.Delete(keyringService, id); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete credential from the operating system keyring: %w", err)
	}
	return nil
}

func EnvironmentSecretRef(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !environmentPattern.MatchString(name) {
		return "", errors.New("credential environment variable must use uppercase letters, digits, and underscores")
	}
	return "env:" + name, nil
}
