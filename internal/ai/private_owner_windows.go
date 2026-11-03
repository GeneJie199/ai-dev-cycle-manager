//go:build windows

package ai

import "errors"

func checkPrivateOwner(string, bool) error {
	return errors.New("private-file credential storage is unsupported on Windows; use the operating system Credential Manager")
}
