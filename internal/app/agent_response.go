package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

const maxAgentResponseBytes int64 = 1 << 20

type responseState struct {
	Complete  bool   `json:"complete"`
	Truncated bool   `json:"truncated"`
	SHA256    string `json:"sha256"`
}

// stdout is a final-response protocol; the merged log remains diagnostic audit.
type responseCapture struct {
	mu                sync.Mutex
	writer            *redactingLogWriter
	path              string
	maximum, received int64
	truncated         bool
	writeErr          error
}

func openResponseCapture(path string, maximum int64) (*responseCapture, error) {
	writer, err := openRedactingLog(path, maximum)
	if err != nil {
		return nil, err
	}
	return &responseCapture{writer: writer, path: path, maximum: maximum}, nil
}

func (capture *responseCapture) Write(data []byte) (int, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	originalLength := len(data)
	remaining := capture.maximum - capture.received
	capture.received += int64(len(data))
	if capture.received > capture.maximum {
		capture.truncated = true
	}
	if remaining <= 0 {
		return originalLength, nil
	}
	if int64(len(data)) > remaining {
		data = data[:int(remaining)]
	}
	_, err := capture.writer.Write(data)
	if err != nil {
		capture.writeErr = err
	}
	return originalLength, err
}

func (capture *responseCapture) Close() error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	target := capture.writer.target
	err := capture.writer.Close()
	if target != nil {
		target.mu.Lock()
		capture.truncated = capture.truncated || target.truncated
		target.mu.Unlock()
	}
	if err == nil {
		err = capture.writeErr
	}
	data, readErr := os.ReadFile(capture.path)
	if err == nil {
		err = readErr
	}
	digest := sha256.Sum256(data)
	state := responseState{Complete: err == nil, Truncated: capture.truncated, SHA256: hex.EncodeToString(digest[:])}
	encoded, marshalErr := json.Marshal(state)
	if marshalErr != nil {
		return marshalErr
	}
	if saveErr := os.WriteFile(capture.path+".json", encoded, 0o600); saveErr != nil {
		return saveErr
	}
	return err
}

// AgentSessionResponse never falls back to merged audit output or a truncated tail.
func (a *App) AgentSessionResponse(ctx context.Context, id string) (string, error) {
	session, err := a.Store.GetAgentSession(ctx, id)
	if err != nil {
		return "", err
	}
	if session.Status != "completed" {
		return "", errors.New("agent response requires a completed session")
	}
	path := session.LogPath + ".response"
	marker, err := os.ReadFile(path + ".json")
	if err != nil {
		return "", fmt.Errorf("complete stdout response unavailable: %w", err)
	}
	var state responseState
	if json.Unmarshal(marker, &state) != nil || !state.Complete || state.Truncated {
		return "", errors.New("stdout response is incomplete or truncated")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAgentResponseBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxAgentResponseBytes {
		return "", errors.New("stdout response exceeds its limit")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != state.SHA256 {
		return "", errors.New("stdout response integrity mismatch")
	}
	return string(data), nil
}
