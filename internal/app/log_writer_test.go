package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBoundedAgentLogKeepsTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	writer, err := openBoundedLog(path, 128)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write(bytes.Repeat([]byte("a"), 100)); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write(bytes.Repeat([]byte("b"), 100)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > 128 {
		t.Fatalf("log size = %d, %v", info.Size(), err)
	}
	tail, err := readLogTail(path, 20)
	if err != nil || !bytes.Equal(tail, bytes.Repeat([]byte("b"), 20)) {
		t.Fatalf("tail = %q, %v", tail, err)
	}
}

func TestBoundedAgentLogHandlesSingleOversizedWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	writer, err := openBoundedLog(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	if n, writeErr := writer.Write([]byte("0123456789abcdef")); writeErr != nil || n != 16 {
		t.Fatalf("Write = %d, %v", n, writeErr)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "89abcdef" {
		t.Fatalf("contents = %q, %v", contents, err)
	}
}

func TestReadLogTailWithZeroLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents, err := readLogTail(path, 0)
	if err != nil || len(contents) != 0 {
		t.Fatalf("contents = %q, %v", contents, err)
	}
}

func TestTailBufferBoundsVerificationOutput(t *testing.T) {
	buffer := &tailBuffer{maximum: 8}
	if n, err := buffer.Write([]byte("012345")); err != nil || n != 6 {
		t.Fatalf("first Write = %d, %v", n, err)
	}
	if n, err := buffer.Write([]byte("6789abcdef")); err != nil || n != 10 {
		t.Fatalf("second Write = %d, %v", n, err)
	}
	if got := buffer.String(); got != truncatedLogMarker+"89abcdef" {
		t.Fatalf("output = %q", got)
	}
}

func TestRedactingLogWriterHandlesSplitSecretsAndPrivateKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "redacted.log")
	writer, err := openRedactingLog(path, 4096)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []string{"password=hun", "ter2\n-----BEGIN PRIVATE KEY-----\n", "private-material\n", "-----END PRIVATE KEY-----\n", "token=last-secret"} {
		if _, err = writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hunter2", "private-material", "last-secret"} {
		if bytes.Contains(contents, []byte(secret)) {
			t.Fatalf("log leaked %q: %s", secret, contents)
		}
	}
	if !bytes.Contains(contents, []byte("[REDACTED PRIVATE KEY]")) {
		t.Fatalf("private key marker missing: %s", contents)
	}
}
