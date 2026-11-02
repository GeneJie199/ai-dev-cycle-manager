package ai

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExplicitPrivateCredentialsPermissionsRevokeAndSymlinkRejection(t *testing.T) {
	s := NewLocalSecretStore(filepath.Join(t.TempDir(), "credentials"))
	ctx := context.Background()
	if runtime.GOOS == "windows" {
		if _, e := s.SetPrivate(ctx, "fixture", "synthetic-private-value"); e == nil {
			t.Fatal("Windows chmod pretended to secure ACL")
		}
		return
	}
	ref, e := s.SetPrivate(ctx, "fixture", "synthetic-private-value")
	if e != nil {
		t.Fatal(e)
	}
	value, e := s.Get(ctx, ref)
	if e != nil || value != "synthetic-private-value" {
		t.Fatal("private secret roundtrip failed")
	}
	if _, e = s.SetPrivate(ctx, "fixture", "replacement"); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(s.Directory, "fixture")
	if e = os.Chmod(file, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, ref); e == nil {
		t.Fatal("world-readable credential accepted")
	}
	os.Chmod(file, 0600)
	if e = s.Delete(ctx, ref); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, ref); e == nil {
		t.Fatal("revoked credential still readable")
	}
	target := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(target, []byte("do not read"), 0600)
	if e = os.Symlink(target, file); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, ref); e == nil {
		t.Fatal("symlink credential read")
	}
	if _, e = s.SetPrivate(ctx, "fixture", "new"); e == nil {
		t.Fatal("symlink credential overwritten")
	}
	if _, e = s.SetPrivate(ctx, "../escape", "new"); e == nil || strings.Contains(e.Error(), "synthetic-private-value") {
		t.Fatal("unsafe ID/secret error")
	}
}
func TestPrivateCredentialDirectorySymlinkAndWidePermissionsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows private backend deliberately unsupported")
	}
	root := t.TempDir()
	target := filepath.Join(root, "actual")
	os.Mkdir(target, 0700)
	link := filepath.Join(root, "link")
	os.Symlink(target, link)
	s := NewLocalSecretStore(link)
	if _, e := s.SetPrivate(context.Background(), "fixture", "synthetic"); e == nil {
		t.Fatal("symlink credential directory accepted")
	}
	s = NewLocalSecretStore(target)
	os.Chmod(target, 0755)
	if _, e := s.SetPrivate(context.Background(), "fixture", "synthetic"); e == nil {
		t.Fatal("wide credential directory accepted")
	}
}
