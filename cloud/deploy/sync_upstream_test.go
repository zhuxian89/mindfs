package deploy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Linux CI exercises util-linux flock; macOS only substitutes the lock syscall.
func maintenanceTestPath(t *testing.T, bin string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		writeExecutable(t, filepath.Join(bin, "flock"), "#!/bin/sh\nexit 0\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

type syncFixture struct {
	repo, origin, upstream, source string
}

func gitAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func syncCommit(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	gitAt(t, dir, "add", file)
	gitAt(t, dir, "commit", "-qm", content)
}

func newSyncFixture(t *testing.T) syncFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Relay test")
	t.Setenv("GIT_AUTHOR_EMAIL", "relay@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Relay test")
	t.Setenv("GIT_COMMITTER_EMAIL", "relay@example.invalid")
	f := syncFixture{filepath.Join(root, "checkout"), filepath.Join(root, "origin.git"), filepath.Join(root, "upstream.git"), filepath.Join(root, "source")}
	gitAt(t, root, "init", "--bare", "--initial-branch=main", f.origin)
	gitAt(t, root, "init", "--bare", "--initial-branch=main", f.upstream)
	gitAt(t, root, "init", "--initial-branch=main", f.source)
	deploy := filepath.Join(f.source, "cloud", "deploy")
	if err := os.MkdirAll(deploy, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(deploy, "sync-upstream.sh"), read(t, "sync-upstream.sh"))
	syncCommit(t, f.source, ".gitignore", "cloud/deploy/.relay-upgrade/\n")
	syncCommit(t, f.source, "shared", "base")
	gitAt(t, f.source, "add", ".")
	gitAt(t, f.source, "commit", "-qm", "sync script")
	gitAt(t, f.source, "push", f.origin, "main")
	gitAt(t, f.source, "push", f.upstream, "main")
	gitAt(t, root, "clone", f.origin, f.repo)
	gitAt(t, f.repo, "remote", "add", "upstream", f.upstream)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(bin, "docker"), "#!/bin/sh\necho 'unexpected Docker call' >&2\nexit 99\n")
	maintenanceTestPath(t, bin)
	return f
}

func (f syncFixture) run(t *testing.T, wantFailure bool) string {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(f.repo, "cloud", "deploy", "sync-upstream.sh"))
	cmd.Dir = t.TempDir() // A scheduler may start outside the checkout.
	out, err := cmd.CombinedOutput()
	if (err != nil) != wantFailure {
		t.Fatalf("sync error=%v, want failure=%v\n%s", err, wantFailure, out)
	}
	return string(out)
}

func TestSyncUpstreamPushesAndRetriesFailedPush(t *testing.T) {
	f := newSyncFixture(t)
	syncCommit(t, f.source, "new", "upstream change")
	gitAt(t, f.source, "push", f.upstream, "main")
	hook := filepath.Join(f.origin, "hooks", "pre-receive")
	writeExecutable(t, hook, "#!/bin/sh\nexit 1\n")
	f.run(t, true)
	want := gitAt(t, f.source, "rev-parse", "HEAD")
	if got := gitAt(t, f.repo, "rev-parse", "HEAD"); got != want {
		t.Fatalf("merge was not retained after rejected push: %s", got)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if out := f.run(t, false); !strings.Contains(out, "SYNC OK") {
		t.Fatal(out)
	}
	if got := gitAt(t, f.origin, "rev-parse", "main"); got != want {
		t.Fatalf("push retry did not publish merged HEAD: %s", got)
	}
	if out := f.run(t, false); !strings.Contains(out, "SYNC SKIPPED") {
		t.Fatal(out)
	}
}

func TestSyncUpstreamProtectsExistingWork(t *testing.T) {
	for _, scenario := range []string{"branch", "dirty", "merge", "rebase", "conflict", "diverged-origin", "untracked-collision"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSyncFixture(t)
			switch scenario {
			case "branch":
				gitAt(t, f.repo, "switch", "-c", "work")
			case "dirty":
				if err := os.WriteFile(filepath.Join(f.repo, "shared"), []byte("local work"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "merge":
				if err := os.WriteFile(filepath.Join(f.repo, ".git", "MERGE_HEAD"), []byte(gitAt(t, f.repo, "rev-parse", "HEAD")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "rebase":
				if err := os.Mkdir(filepath.Join(f.repo, ".git", "rebase-merge"), 0o700); err != nil {
					t.Fatal(err)
				}
			case "conflict", "diverged-origin":
				syncCommit(t, f.repo, "shared", "local commit")
				syncCommit(t, f.source, "shared", "remote conflict")
				remote := f.upstream
				if scenario == "diverged-origin" {
					remote = f.origin
				}
				gitAt(t, f.source, "push", remote, "main")
			case "untracked-collision":
				syncCommit(t, f.source, "new", "remote file")
				gitAt(t, f.source, "push", f.upstream, "main")
				if err := os.WriteFile(filepath.Join(f.repo, "new"), []byte("untracked work"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			beforeHead := gitAt(t, f.repo, "rev-parse", "HEAD")
			beforeStatus := gitAt(t, f.repo, "status", "--porcelain")
			beforeOrigin := gitAt(t, f.origin, "rev-parse", "main")
			beforeShared := read(t, filepath.Join(f.repo, "shared"))
			f.run(t, true)
			if gitAt(t, f.repo, "rev-parse", "HEAD") != beforeHead || gitAt(t, f.origin, "rev-parse", "main") != beforeOrigin || gitAt(t, f.repo, "status", "--porcelain") != beforeStatus || read(t, filepath.Join(f.repo, "shared")) != beforeShared {
				t.Fatal("failed sync changed existing work or published commits")
			}
			if scenario == "merge" {
				if _, err := os.Stat(filepath.Join(f.repo, ".git", "MERGE_HEAD")); err != nil {
					t.Fatal("existing merge was aborted")
				}
			}
		})
	}
}

func TestSyncUpstreamFastForwardsOriginBeforeMerging(t *testing.T) {
	f := newSyncFixture(t)
	syncCommit(t, f.source, "fork", "fork change")
	gitAt(t, f.source, "push", f.origin, "main")
	f.run(t, false)
	if gitAt(t, f.repo, "rev-parse", "HEAD") != gitAt(t, f.origin, "rev-parse", "main") {
		t.Fatal("checkout did not follow own main")
	}
}
