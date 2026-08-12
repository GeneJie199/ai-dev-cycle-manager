package ai

import (
	"strings"
	"testing"
)

func TestExtractJSONObjectFromFencedNoisyOutput(t *testing.T) {
	var result struct {
		Items []string `json:"items"`
	}
	output := "analysis first\n```json\n{\"items\":[\"one\",\"brace } in string\"]}\n```\ndone"
	if err := ExtractJSONObject(output, &result); err != nil || len(result.Items) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestExtractJSONObjectRejectsUnknownFields(t *testing.T) {
	var result struct {
		Summary string `json:"summary"`
	}
	if err := ExtractJSONObject(`{"summary":"ok","invented":true}`, &result); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}

func TestExtractJSONObjectIsolatesFailedCandidatesAndChoosesLargest(t *testing.T) {
	type response struct {
		Criteria []string `json:"criteria"`
		Tasks    []string `json:"tasks"`
	}
	result := response{Criteria: []string{"existing"}}
	output := `analysis {"criteria":[],"unknown":true} then {} then {"criteria":["criterion"],"tasks":["task"]} trailing`
	if err := ExtractJSONObject(output, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Criteria) != 1 || result.Criteria[0] != "criterion" || len(result.Tasks) != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestGenerationCapacityIsBounded(t *testing.T) {
	service := NewService()
	if !service.acquire() {
		t.Fatal("first slot should be available")
	}
	if !service.acquire() {
		t.Fatal("first two slots should be available")
	}
	if service.acquire() {
		t.Fatal("third slot should be rejected")
	}
	service.release()
	service.release()
}

func TestRedactRemovesCommonSecrets(t *testing.T) {
	input := "token=abc123 password hunter2 postgres://user:pass@db/x\n-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----"
	redacted, count := Redact(input)
	for _, secret := range []string{"abc123", "hunter2", ":pass@", "PRIVATE KEY-----\nsecret"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redaction leaked %q: %s", secret, redacted)
		}
	}
	if count != 4 {
		t.Fatalf("redaction count=%d output=%s", count, redacted)
	}
}

func TestLimitedBufferBoundsMemory(t *testing.T) {
	buffer := &limitedBuffer{maximum: 4}
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 || buffer.String() != "abcd" || !buffer.exceeded {
		t.Fatalf("buffer=%q written=%d exceeded=%v err=%v", buffer.String(), written, buffer.exceeded, err)
	}
}
