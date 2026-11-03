//go:build !windows

package ai

import (
	"errors"
	"os"
	"syscall"
)

func checkPrivateOwner(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return errors.New("private credential storage unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) {
		return errors.New("private credential storage must be current-user owned, regular and not a symlink")
	}
	expected := os.FileMode(0600)
	if directory {
		expected = 0700
	}
	if info.Mode().Perm() != expected {
		return errors.New("private credential storage requires directory 0700 and file 0600")
	}
	return nil
}
