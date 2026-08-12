// Command devcycle is a CLI demo / e2e harness for the ai-dev-cycle-manager backend.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/app"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/git"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/web"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	ctx := context.Background()

	switch cmd {
	case "version", "--version":
		fmt.Println(version)
	case "demo":
		if err := runDemo(ctx, args); err != nil {
			fatal(err)
		}
	case "import":
		if err := runImport(ctx, args); err != nil {
			fatal(err)
		}
	case "status":
		if err := runStatus(ctx, args); err != nil {
			fatal(err)
		}
	case "branches":
		if err := runBranches(ctx, args); err != nil {
			fatal(err)
		}
	case "log":
		if err := runLog(ctx, args); err != nil {
			fatal(err)
		}
	case "diff":
		if err := runDiff(ctx, args); err != nil {
			fatal(err)
		}
	case "worktree":
		if err := runWorktree(ctx, args); err != nil {
			fatal(err)
		}
	case "requirement":
		if err := runRequirement(ctx, args); err != nil {
			fatal(err)
		}
	case "task":
		if err := runTask(ctx, args); err != nil {
			fatal(err)
		}
	case "serve":
		if err := runServe(ctx, args); err != nil {
			fatal(err)
		}
	case "evidence":
		if err := runEvidence(ctx, args); err != nil {
			fatal(err)
		}
	case "verify":
		if err := runVerify(ctx, args); err != nil {
			fatal(err)
		}
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `devcycle — AI Dev Cycle Manager (Go backend + CLI)

Terminology:
  Branch   = 独立工作版本
  Worktree = 隔离开发目录

Usage:
  devcycle version
  devcycle demo [--repo PATH] [--db PATH]
      End-to-end demo: init temp git repo (or use --repo), import, create
      requirement + criteria + task, create branch/worktree, print summary.

  devcycle import --repo PATH [--db PATH]
  devcycle status --repo PATH
  devcycle branches --repo PATH [--remote]
  devcycle log --repo PATH [-n N]
  devcycle diff --repo PATH [--stat] [--staged]
  devcycle worktree list --repo PATH
  devcycle worktree add --repo PATH --path WT --branch NAME [--create-branch]
  devcycle worktree remove --repo PATH --path WT [--force]
  devcycle requirement create --title T [--desc D] [--db PATH]
  devcycle requirement list [--db PATH]
  devcycle requirement export --id ID [--out FILE] [--db PATH]
  devcycle requirement report --id ID [--out FILE] [--db PATH]
  devcycle evidence add --req ID --kind test --title T [--criterion ID --inline TEXT --uri PATH]
  devcycle evidence list --req ID [--db PATH]
  devcycle verify --req ID --name test --command "go test ./..." --cwd PATH [--criterion ID]
  devcycle task create --req ID --title T [--desc D] [--db PATH]
  devcycle task link --repo PATH --task ID --branch B --path WT [--db PATH]
  devcycle task list [--db PATH]
  devcycle serve [--db PATH] [--addr 127.0.0.1:8766]
      Local web workbench (UI + JSON API). Loopback only.

Default DB: ./devcycle.db (override with --db or DEVCYCLE_DB).
`)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func defaultDB(fs *flag.FlagSet) *string {
	def := os.Getenv("DEVCYCLE_DB")
	if def == "" {
		def = "devcycle.db"
	}
	return fs.String("db", def, "SQLite database path")
}

func openApp(dbPath string) (*app.App, error) {
	return app.New(dbPath)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	repo := fs.String("repo", "", "git repository path")
	dbPath := defaultDB(fs)
	_ = fs.Parse(args)
	if *repo == "" {
		return fmt.Errorf("--repo is required")
	}
	a, err := openApp(*dbPath)
	if err != nil {
		return err
	}
	defer a.Close()
	r, err := a.ImportRepository(ctx, *repo)
	if err != nil {
		return err
	}
	printJSON(r)
	return nil
}

func runStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	repo := fs.String("repo", "", "git repository path")
	_ = fs.Parse(args)
	if *repo == "" {
		return fmt.Errorf("--repo is required")
	}
	st, err := git.NewClient(*repo).Status(ctx)
	if err != nil {
		return err
	}
	printJSON(st)
	return nil
}

func runBranches(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("branches", flag.ExitOnError)
	repo := fs.String("repo", "", "git repository path")
	remote := fs.Bool("remote", false, "include remote branches")
	_ = fs.Parse(args)
	if *repo == "" {
		return fmt.Errorf("--repo is required")
	}
	list, err := git.NewClient(*repo).Branches(ctx, *remote)
	if err != nil {
		return err
	}
	printJSON(list)
	return nil
}

func runLog(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	repo := fs.String("repo", "", "git repository path")
	n := fs.Int("n", 10, "max commits")
	_ = fs.Parse(args)
	if *repo == "" {
		return fmt.Errorf("--repo is required")
	}
	list, err := git.NewClient(*repo).Log(ctx, *n)
	if err != nil {
		return err
	}
	printJSON(list)
	return nil
}

func runDiff(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	repo := fs.String("repo", "", "git repository path")
	stat := fs.Bool("stat", false, "show --stat")
	staged := fs.Bool("staged", false, "show staged diff")
	_ = fs.Parse(args)
	if *repo == "" {
		return fmt.Errorf("--repo is required")
	}
	res, err := git.NewClient(*repo).Diff(ctx, git.DiffOptions{Stat: *stat, Staged: *staged})
	if err != nil {
		return err
	}
	if res.Content == "" {
		fmt.Println("(no changes)")
		return nil
	}
	fmt.Println(res.Content)
	return nil
}

func runWorktree(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: worktree list|add|remove")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		fs := flag.NewFlagSet("worktree list", flag.ExitOnError)
		repo := fs.String("repo", "", "git repository path")
		_ = fs.Parse(rest)
		if *repo == "" {
			return fmt.Errorf("--repo is required")
		}
		list, err := git.NewClient(*repo).ListWorktrees(ctx)
		if err != nil {
			return err
		}
		printJSON(list)
		return nil
	case "add":
		fs := flag.NewFlagSet("worktree add", flag.ExitOnError)
		repo := fs.String("repo", "", "git repository path")
		path := fs.String("path", "", "worktree path (隔离开发目录)")
		branch := fs.String("branch", "", "branch name (独立工作版本)")
		create := fs.Bool("create-branch", false, "create new branch")
		_ = fs.Parse(rest)
		if *repo == "" || *path == "" {
			return fmt.Errorf("--repo and --path are required")
		}
		info, err := git.NewClient(*repo).AddWorktree(ctx, git.WorktreeAddOptions{
			Path:         *path,
			Branch:       *branch,
			CreateBranch: *create,
			StartPoint:   "HEAD",
		})
		if err != nil {
			return err
		}
		printJSON(info)
		return nil
	case "remove":
		fs := flag.NewFlagSet("worktree remove", flag.ExitOnError)
		repo := fs.String("repo", "", "git repository path")
		path := fs.String("path", "", "worktree path")
		force := fs.Bool("force", false, "force remove")
		_ = fs.Parse(rest)
		if *repo == "" || *path == "" {
			return fmt.Errorf("--repo and --path are required")
		}
		return git.NewClient(*repo).RemoveWorktree(ctx, *path, *force)
	default:
		return fmt.Errorf("unknown worktree subcommand: %s", sub)
	}
}

func runRequirement(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: requirement create|list|export")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "create":
		fs := flag.NewFlagSet("requirement create", flag.ExitOnError)
		title := fs.String("title", "", "title")
		desc := fs.String("desc", "", "description")
		dbPath := defaultDB(fs)
		_ = fs.Parse(rest)
		if *title == "" {
			return fmt.Errorf("--title is required")
		}
		a, err := openApp(*dbPath)
		if err != nil {
			return err
		}
		defer a.Close()
		req, err := a.CreateRequirement(ctx, *title, *desc)
		if err != nil {
			return err
		}
		printJSON(req)
		return nil
	case "list":
		fs := flag.NewFlagSet("requirement list", flag.ExitOnError)
		dbPath := defaultDB(fs)
		_ = fs.Parse(rest)
		a, err := openApp(*dbPath)
		if err != nil {
			return err
		}
		defer a.Close()
		list, err := a.ListRequirements(ctx)
		if err != nil {
			return err
		}
		printJSON(list)
		return nil
	case "export":
		fs := flag.NewFlagSet("requirement export", flag.ExitOnError)
		id := fs.String("id", "", "requirement id")
		out := fs.String("out", "", "output file (stdout when empty)")
		dbPath := defaultDB(fs)
		_ = fs.Parse(rest)
		if *id == "" {
			return fmt.Errorf("--id is required")
		}
		a, err := openApp(*dbPath)
		if err != nil {
			return err
		}
		defer a.Close()
		candidate, err := a.ExportReleaseCandidate(ctx, *id)
		if err != nil {
			return err
		}
		if *out == "" {
			printJSON(candidate)
			return nil
		}
		b, err := json.MarshalIndent(candidate, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(*out, append(b, '\n'), 0o600)
	case "report":
		fs := flag.NewFlagSet("requirement report", flag.ExitOnError)
		id := fs.String("id", "", "requirement id")
		out := fs.String("out", "", "output file (stdout when empty)")
		dbPath := defaultDB(fs)
		_ = fs.Parse(rest)
		if *id == "" {
			return fmt.Errorf("--id is required")
		}
		a, err := openApp(*dbPath)
		if err != nil {
			return err
		}
		defer a.Close()
		report, err := a.DevelopmentReport(ctx, *id)
		if err != nil {
			return err
		}
		if *out == "" {
			printJSON(report)
			return nil
		}
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(*out, append(b, '\n'), 0o600)
	default:
		return fmt.Errorf("unknown requirement subcommand: %s", sub)
	}
}

func runEvidence(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: evidence add|list")
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("evidence add", flag.ExitOnError)
		req := fs.String("req", "", "requirement id")
		criterion := fs.String("criterion", "", "criterion id")
		task := fs.String("task", "", "task id")
		kind := fs.String("kind", "manual", "evidence kind")
		title := fs.String("title", "", "title")
		status := fs.String("status", "passed", "passed, failed, or informational")
		uri := fs.String("uri", "", "artifact URI/path")
		inline := fs.String("inline", "", "inline evidence text")
		dbPath := defaultDB(fs)
		_ = fs.Parse(args[1:])
		a, err := openApp(*dbPath)
		if err != nil {
			return err
		}
		defer a.Close()
		e, err := a.AddEvidence(ctx, models.Evidence{RequirementID: *req, CriterionID: *criterion, TaskID: *task, Kind: *kind, Title: *title, Status: *status, URI: *uri, Inline: *inline})
		if err != nil {
			return err
		}
		printJSON(e)
		return nil
	case "list":
		fs := flag.NewFlagSet("evidence list", flag.ExitOnError)
		req := fs.String("req", "", "requirement id")
		dbPath := defaultDB(fs)
		_ = fs.Parse(args[1:])
		if *req == "" {
			return fmt.Errorf("--req is required")
		}
		a, err := openApp(*dbPath)
		if err != nil {
			return err
		}
		defer a.Close()
		items, err := a.ListEvidence(ctx, *req)
		if err != nil {
			return err
		}
		printJSON(items)
		return nil
	default:
		return fmt.Errorf("unknown evidence subcommand: %s", args[0])
	}
}
func runVerify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	req := fs.String("req", "", "requirement id")
	criterion := fs.String("criterion", "", "criterion id")
	name := fs.String("name", "verification", "run name")
	command := fs.String("command", "", "command to run")
	cwd := fs.String("cwd", ".", "working directory")
	timeout := fs.Duration("timeout", 10*time.Minute, "timeout")
	dbPath := defaultDB(fs)
	_ = fs.Parse(args)
	a, err := openApp(*dbPath)
	if err != nil {
		return err
	}
	defer a.Close()
	run, err := a.RunVerification(ctx, *req, *criterion, *name, *command, *cwd, *timeout)
	printJSON(run)
	return err
}

func runTask(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: task create|link|list")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "create":
		fs := flag.NewFlagSet("task create", flag.ExitOnError)
		reqID := fs.String("req", "", "requirement id")
		title := fs.String("title", "", "title")
		desc := fs.String("desc", "", "description")
		dbPath := defaultDB(fs)
		_ = fs.Parse(rest)
		if *reqID == "" || *title == "" {
			return fmt.Errorf("--req and --title are required")
		}
		a, err := openApp(*dbPath)
		if err != nil {
			return err
		}
		defer a.Close()
		task, err := a.CreateTask(ctx, *reqID, *title, *desc)
		if err != nil {
			return err
		}
		printJSON(task)
		return nil
	case "link":
		fs := flag.NewFlagSet("task link", flag.ExitOnError)
		repo := fs.String("repo", "", "git repository path")
		taskID := fs.String("task", "", "task id")
		branch := fs.String("branch", "", "branch (独立工作版本)")
		path := fs.String("path", "", "worktree path (隔离开发目录)")
		dbPath := defaultDB(fs)
		_ = fs.Parse(rest)
		if *repo == "" || *taskID == "" || *branch == "" || *path == "" {
			return fmt.Errorf("--repo, --task, --branch, and --path are required")
		}
		a, err := openApp(*dbPath)
		if err != nil {
			return err
		}
		defer a.Close()
		task, wt, err := a.LinkTaskToWorktree(ctx, *repo, *taskID, *branch, *path)
		if err != nil {
			return err
		}
		printJSON(map[string]any{"task": task, "worktree": wt})
		return nil
	case "list":
		fs := flag.NewFlagSet("task list", flag.ExitOnError)
		dbPath := defaultDB(fs)
		_ = fs.Parse(rest)
		a, err := openApp(*dbPath)
		if err != nil {
			return err
		}
		defer a.Close()
		list, err := a.ListTasks(ctx)
		if err != nil {
			return err
		}
		printJSON(list)
		return nil
	default:
		return fmt.Errorf("unknown task subcommand: %s", sub)
	}
}

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dbPath := defaultDB(fs)
	addr := fs.String("addr", "127.0.0.1:8766", "loopback listen address")
	_ = fs.Parse(args)

	a, err := openApp(*dbPath)
	if err != nil {
		return err
	}
	defer a.Close()

	srv, err := web.NewServer(a, log.New(os.Stderr, "[devcycle] ", log.LstdFlags))
	if err != nil {
		return err
	}
	fmt.Printf("DB: %s\nOpen http://%s in your browser (Ctrl+C to stop)\n", a.DBPath, *addr)
	serveCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Serve(serveCtx, *addr)
}

func runDemo(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("demo", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "existing git repo (optional; temp repo created if empty)")
	dbPath := fs.String("db", "", "sqlite db path (default: temp)")
	_ = fs.Parse(args)

	workRoot := filepath.Join(os.TempDir(), fmt.Sprintf("devcycle-demo-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return err
	}

	repoPath := *repoFlag
	cleanupRepo := false
	if repoPath == "" {
		repoPath = filepath.Join(workRoot, "repo")
		if err := initDemoRepo(repoPath); err != nil {
			return fmt.Errorf("init temp repo: %w", err)
		}
		cleanupRepo = true
		fmt.Printf("Created temp git repo at %s\n", repoPath)
	} else {
		abs, err := filepath.Abs(repoPath)
		if err != nil {
			return err
		}
		repoPath = abs
		fmt.Printf("Using existing repo at %s\n", repoPath)
	}

	db := *dbPath
	if db == "" {
		db = filepath.Join(workRoot, "demo.db")
	}
	a, err := openApp(db)
	if err != nil {
		return err
	}
	defer a.Close()

	repo, err := a.ImportRepository(ctx, repoPath)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	fmt.Printf("Imported repository: %s (%s)\n", repo.Name, repo.Path)

	req, err := a.CreateRequirement(ctx, "Demo requirement", "Exercise DEV-001..005 via CLI")
	if err != nil {
		return err
	}
	crit, err := a.CreateCriterion(ctx, req.ID, "Worktree and task are linked")
	if err != nil {
		return err
	}
	task, err := a.CreateTask(ctx, req.ID, "Implement demo flow", "Create branch + worktree")
	if err != nil {
		return err
	}

	branch := "feature/demo-" + time.Now().Format("150405")
	// Sanitize branch for filesystem path (Branch may contain '/' e.g. feature/foo).
	wtDir := strings.ReplaceAll(branch, "/", "-")
	wtPath := filepath.Join(workRoot, "worktrees", wtDir)
	task, wt, err := a.LinkTaskToWorktree(ctx, repoPath, task.ID, branch, wtPath)
	if err != nil {
		return fmt.Errorf("link task/worktree: %w", err)
	}

	st, err := a.GitStatus(ctx, repoPath)
	if err != nil {
		return err
	}
	commits, err := a.GitLog(ctx, repoPath, 3)
	if err != nil {
		return err
	}
	wts, err := a.ListWorktrees(ctx, repoPath)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("=== Demo summary ===")
	fmt.Printf("DB:            %s\n", db)
	fmt.Printf("Repo:          %s\n", repo.Path)
	fmt.Printf("Requirement:   %s — %s\n", req.ID, req.Title)
	fmt.Printf("Criterion:     %s — %s\n", crit.ID, crit.Description)
	fmt.Printf("Task:          %s — %s [%s]\n", task.ID, task.Title, task.Status)
	fmt.Printf("Branch:        %s  (独立工作版本)\n", task.Branch)
	fmt.Printf("Worktree:      %s  (隔离开发目录)\n", wt.Path)
	fmt.Printf("Git status:    branch=%s clean=%v\n", st.Branch, st.Clean)
	fmt.Printf("Recent commit: %s %s\n", commits[0].ShortHash, commits[0].Subject)
	fmt.Printf("Worktrees:     %d\n", len(wts))
	fmt.Println()
	fmt.Println("Note: Codex adapter is interface-only; no agent was started.")
	if cleanupRepo {
		fmt.Printf("Temp files under %s (not deleted; remove manually if desired)\n", workRoot)
	}
	return nil
}

func initDemoRepo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	run := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=Demo",
			"GIT_AUTHOR_EMAIL=demo@example.com",
			"GIT_COMMITTER_NAME=Demo",
			"GIT_COMMITTER_EMAIL=demo@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	if err := run("init"); err != nil {
		return err
	}
	_ = run("checkout", "-b", "main")
	if err := run("config", "user.name", "Demo"); err != nil {
		return err
	}
	if err := run("config", "user.email", "demo@example.com"); err != nil {
		return err
	}
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# demo repo\n"), 0o644); err != nil {
		return err
	}
	if err := run("add", "README.md"); err != nil {
		return err
	}
	return run("commit", "-m", "initial commit")
}
