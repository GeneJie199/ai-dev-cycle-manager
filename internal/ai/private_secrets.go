package ai

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalSecretStore keeps the existing OS keyring/environment implementation.
// Private files are an explicit, owner-only local option, not OS encryption.
type LocalSecretStore struct {
	Keyring   SecretStore
	Directory string
}

func NewLocalSecretStore(directory string) *LocalSecretStore {
	return &LocalSecretStore{Keyring: NewKeyringSecretStore(), Directory: directory}
}
func (s *LocalSecretStore) Set(ctx context.Context, id, value string) (string, error) {
	return s.Keyring.Set(ctx, id, value)
}
func (s *LocalSecretStore) privatePath(id string, create bool) (string, error) {
	if !secretIDPattern.MatchString(id) {
		return "", errors.New("invalid private credential ID")
	}
	directory, err := filepath.Abs(s.Directory)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(directory))
	if err != nil || filepath.Clean(parent) != filepath.Clean(filepath.Dir(directory)) {
		return "", errors.New("private credential directory must not have symlink ancestors")
	}
	if create {
		if err = os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	if err = checkPrivateOwner(directory, true); err != nil {
		return "", err
	}
	path := filepath.Join(directory, id)
	if _, err = os.Lstat(path); err == nil {
		if err = checkPrivateOwner(path, false); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return path, nil
}
func (s *LocalSecretStore) SetPrivate(ctx context.Context, id, value string) (string, error) {
	if strings.TrimSpace(value) == "" || len(value) > 16384 {
		return "", errors.New("private credential value is empty or too large")
	}
	path, err := s.privatePath(id, true)
	if err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(s.Directory, ".credential-")
	if err != nil {
		return "", errors.New("private credential creation failed")
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.WriteString(value)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", errors.New("private credential write failed")
	}
	if err = os.Rename(name, path); err != nil {
		return "", errors.New("private credential replacement failed")
	}
	return "private:" + id, nil
}
func (s *LocalSecretStore) Get(ctx context.Context, ref string) (string, error) {
	if !strings.HasPrefix(ref, "private:") {
		return s.Keyring.Get(ctx, ref)
	}
	path, err := s.privatePath(strings.TrimPrefix(ref, "private:"), false)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", errors.New("private credential unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > 16384 {
		return "", errors.New("private credential invalid")
	}
	data := make([]byte, info.Size())
	if _, err = io.ReadFull(f, data); err != nil {
		return "", errors.New("private credential read failed")
	}
	return string(data), nil
}
func (s *LocalSecretStore) Delete(ctx context.Context, ref string) error {
	if !strings.HasPrefix(ref, "private:") {
		return s.Keyring.Delete(ctx, ref)
	}
	path, err := s.privatePath(strings.TrimPrefix(ref, "private:"), false)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
