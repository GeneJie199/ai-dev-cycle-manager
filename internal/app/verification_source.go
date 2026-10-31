package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	devgit "github.com/GeneJie199/ai-dev-cycle-manager/internal/git"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

// Source provenance lives in existing evidence metadata, preserving schema 3.
func (a *App) verificationSource(dir string) devgit.SourceState {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	state, err := devgit.InspectSource(ctx, a.Git, dir)
	if err != nil {
		return devgit.SourceState{}
	}
	return state
}

func verificationSourceMetadata(before, after devgit.SourceState) map[string]string {
	verified := before.HeadCommit != "" && before.HeadCommit == after.HeadCommit && before.Clean && after.Clean && before.RepositoryPath == after.RepositoryPath && before.WorktreePath == after.WorktreePath
	return map[string]string{
		"sourceRepository": before.RepositoryPath, "sourceWorktree": before.WorktreePath,
		"sourceCommit": before.HeadCommit, "sourceClean": fmt.Sprint(before.HeadCommit != "" && before.Clean),
		"sourceRepositoryAfter": after.RepositoryPath, "sourceWorktreeAfter": after.WorktreePath,
		"sourceCommitAfter": after.HeadCommit, "sourceCleanAfter": fmt.Sprint(after.HeadCommit != "" && after.Clean),
		"sourceVerified": fmt.Sprint(verified),
	}
}

func verificationSourceChanged(before, after devgit.SourceState) bool {
	// A non-Git command may run, but it cannot claim source-bound proof.
	return before.HeadCommit != "" && verificationSourceMetadata(before, after)["sourceVerified"] != "true"
}

func reservedVerificationMetadata(key string) bool {
	switch key {
	case "verificationRunId", "sourceRepository", "sourceWorktree", "sourceCommit", "sourceClean", "sourceRepositoryAfter", "sourceWorktreeAfter", "sourceCommitAfter", "sourceCleanAfter", "sourceVerified":
		return true
	case "logicalCheckId", "planTaskId", "verificationAttempt", "verificationCheckIndex":
		return true
	}
	return false
}

type verificationBinding struct {
	MissionID, PlanTaskID, TaskID, ExecutionID string
	Attempt, CheckIndex                        int
}

func (b verificationBinding) addMetadata(metadata map[string]string, command string) {
	digest := sha256.Sum256([]byte(strings.Join([]string{b.MissionID, b.PlanTaskID, strconv.Itoa(b.CheckIndex), command}, "\x00")))
	metadata["logicalCheckId"] = hex.EncodeToString(digest[:])
	metadata["missionId"], metadata["planTaskId"] = b.MissionID, b.PlanTaskID
	metadata["verificationAttempt"], metadata["verificationCheckIndex"] = strconv.Itoa(b.Attempt), strconv.Itoa(b.CheckIndex)
	metadata["executionId"] = b.ExecutionID
}

func machineProofScope(proof models.Evidence) (key string, attempt int, managed, valid bool) {
	meta := proof.Metadata
	if meta["logicalCheckId"] == "" && meta["planTaskId"] == "" && meta["verificationAttempt"] == "" {
		return strings.Join([]string{"legacy", meta["command"], verificationPathKey(meta["workingDir"]), proof.TaskID}, "\x00"), 0, false, true
	}
	digest, digestErr := hex.DecodeString(meta["logicalCheckId"])
	attempt, err := strconv.Atoi(meta["verificationAttempt"])
	checkIndex, indexErr := strconv.Atoi(meta["verificationCheckIndex"])
	if digestErr != nil || len(digest) != 32 || err != nil || attempt < 1 || indexErr != nil || checkIndex < 0 || meta["missionId"] == "" || meta["planTaskId"] == "" || meta["executionId"] == "" || meta["verificationRunId"] == "" || proof.TaskID == "" {
		return "", 0, true, false
	}
	return strings.Join([]string{"managed", meta["missionId"], meta["planTaskId"], proof.TaskID, meta["logicalCheckId"]}, "\x00"), attempt, true, true
}

func verificationPathKey(dir string) string {
	clean := filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		clean = strings.ToLower(clean)
	}
	return clean
}

func (a *App) currentMachineEvidence(ctx context.Context, evidence []models.Evidence, runs []models.VerificationRun, sources []SourceSnapshot) bool {
	byEvidence := map[string]models.VerificationRun{}
	for _, run := range runs {
		if run.Status == "running" || run.Status == "stopping" {
			return false
		}
		byEvidence[run.EvidenceID] = run
	}
	latest := map[string]models.Evidence{}
	for _, proof := range evidence { // Store returns newest first with a rowid tie-breaker.
		_, machine := byEvidence[proof.ID]
		if !machine {
			continue
		}
		key, attempt, managed, valid := machineProofScope(proof)
		if !valid {
			return false
		}
		if previous, exists := latest[key]; exists {
			if !managed {
				continue
			}
			if previous.Metadata["command"] != proof.Metadata["command"] {
				return false
			}
			_, previousAttempt, _, _ := machineProofScope(previous)
			if attempt < previousAttempt || (attempt == previousAttempt && proof.CreatedAt.Before(previous.CreatedAt)) {
				continue
			}
			if attempt == previousAttempt && proof.CreatedAt.Equal(previous.CreatedAt) && (proof.Status == "passed" || previous.Status != "passed") {
				continue
			}
		}
		latest[key] = proof
	}
	verifiedSources := []SourceSnapshot{}
	for _, proof := range latest {
		run := byEvidence[proof.ID]
		meta := proof.Metadata
		if run.Status != "passed" || proof.Status != "passed" || meta["verificationRunId"] != run.ID || meta["command"] != run.Command || meta["workingDir"] != run.WorkingDir || meta["sourceVerified"] != "true" || meta["sourceClean"] != "true" || meta["sourceCleanAfter"] != "true" || meta["sourceCommit"] == "" || meta["sourceCommit"] != meta["sourceCommitAfter"] || meta["sourceRepository"] != meta["sourceRepositoryAfter"] || meta["sourceWorktree"] != meta["sourceWorktreeAfter"] {
			return false
		}
		matched := false
		for _, source := range sources {
			if source.Clean && verificationPathKey(source.RepositoryPath) == verificationPathKey(meta["sourceRepository"]) && verificationPathKey(source.WorktreePath) == verificationPathKey(meta["sourceWorktree"]) && source.HeadCommit == meta["sourceCommit"] {
				verifiedSources = append(verifiedSources, source)
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(verifiedSources) == 0 {
		return false
	}
	for _, source := range sources {
		covered := false
		for _, verified := range verifiedSources {
			if verificationPathKey(source.RepositoryPath) != verificationPathKey(verified.RepositoryPath) {
				continue
			}
			if source.HeadCommit == verified.HeadCommit {
				covered = true
				break
			}
			if _, err := missionGit(ctx, verified.WorktreePath, "merge-base", "--is-ancestor", source.HeadCommit, verified.HeadCommit); err == nil {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}
