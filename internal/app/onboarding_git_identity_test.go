package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWebCreatedProjectCanCommitTaskWithoutAmbientGitIdentity(t *testing.T) {
	for _, fixture := range []struct {
		name, globalName, globalEmail string
	}{
		{name: "empty-home"},
		{name: "existing-user-identity", globalName: "Fixture configured user", globalEmail: "fixture-user@example.invalid"},
		{name: "existing-name-missing-email", globalName: "Fixture configured user"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			userDirectory := t.TempDir()
			globalConfig := filepath.Join(userDirectory, ".gitconfig")
			for key, value := range map[string]string{"HOME": userDirectory, "USERPROFILE": userDirectory, "XDG_CONFIG_HOME": userDirectory, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": globalConfig, "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "user.useConfigOnly", "GIT_CONFIG_VALUE_0": "true"} {
				t.Setenv(key, value)
			}
			for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL", "GIT_CONFIG_PARAMETERS"} {
				t.Setenv(key, "")
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			var globalBytes []byte
			if fixture.globalName != "" {
				globalBytes = []byte("[user]\n\tname = " + fixture.globalName + "\n")
				if fixture.globalEmail != "" {
					globalBytes = append(globalBytes, []byte("\temail = "+fixture.globalEmail+"\n")...)
				}
				if err := os.WriteFile(globalConfig, globalBytes, 0600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := New(filepath.Join(t.TempDir(), "identity.db"))
			if err != nil {
				t.Fatal(err)
			}
			wantName, wantEmail := "DevCycle local", "devcycle@example.invalid"
			if fixture.globalName != "" {
				wantName = fixture.globalName
			}
			if fixture.globalEmail != "" {
				wantEmail = fixture.globalEmail
			}
			defer a.Close()
			ctx := context.Background()
			repository, err := a.CreateOnboardingProject(ctx, filepath.Join(t.TempDir(), "new-project"))
			if err != nil {
				t.Fatal(err)
			}
			initialAuthor, err := missionGit(ctx, repository.Path, "log", "-1", "--format=%an|%ae")
			if err != nil || initialAuthor != wantName+"|"+wantEmail {
				t.Fatalf("initial commit did not use the effective project identity: %q %v", initialAuthor, err)
			}
			requirement, err := a.CreateRequirement(ctx, "Synthetic onboarding identity", "local Git fixture")
			if err != nil {
				t.Fatal(err)
			}
			task, err := a.CreateTask(ctx, requirement.ID, "Follow-up edit", "local Git fixture")
			if err != nil {
				t.Fatal(err)
			}
			task, _, err = a.LinkTaskToWorktree(ctx, repository.Path, task.ID, "identity-follow-up", filepath.Join(t.TempDir(), "worktree"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(task.WorktreePath, "README.md"), []byte("# Follow-up task change\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = a.gitClient(task.WorktreePath).CommitTaskChanges(ctx, "Commit onboarding task change", []string{"README.md"}); err != nil {
				t.Fatalf("real controller commit failed after Web project creation: %v", err)
			}
			for key, want := range map[string]string{"user.name": wantName, "user.email": wantEmail} {
				got, err := missionGit(ctx, task.WorktreePath, "config", "--get", key)
				if err != nil || got != want {
					t.Fatalf("worktree identity %s: %q %v", key, got, err)
				}
			}
			if fixture.globalName != "" {
				if raw, err := os.ReadFile(globalConfig); err != nil || string(raw) != string(globalBytes) {
					t.Fatal("existing user Git configuration changed", err)
				}
				if _, err := missionGit(ctx, repository.Path, "config", "--local", "--get", "user.name"); err == nil {
					t.Fatal("new project overwrote an existing effective identity")
				}
			} else if _, err := os.Stat(globalConfig); !os.IsNotExist(err) {
				t.Fatal("project setup created a user-global Git configuration", err)
			}
		})
	}
}
