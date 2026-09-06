package deploy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshAssetsStopsOnFailureAndDoesNotRestartRelay(t *testing.T) {
	script, err := filepath.Abs("refresh-assets.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, fail := range []string{"", "config", "sync", "check"} {
		t.Run("failure="+fail, func(t *testing.T) {
			dir := t.TempDir()
			fakeDocker := `#!/bin/sh
printf '%s\n' "$*" >> "$REFRESH_TEST_LOG"
case "$*" in
  'compose config --quiet') stage=config ;;
  'compose run --rm --no-deps asset-sync') stage=sync ;;
  'compose run --rm --no-deps relay check-assets /var/lib/mindfs-assets') stage=check ;;
  *) exit 99 ;;
esac
test "$stage" != "$REFRESH_TEST_FAIL"
`
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fakeDocker), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			log := filepath.Join(dir, "calls")
			t.Setenv("REFRESH_TEST_LOG", log)
			t.Setenv("REFRESH_TEST_FAIL", fail)
			cmd := exec.Command("/bin/sh", script)
			cmd.Dir = dir // A scheduler need not start in cloud/deploy.
			output, err := cmd.CombinedOutput()
			if (err != nil) != (fail != "") {
				t.Fatalf("result=%v output=%s", err, output)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			wantCalls := 3
			if fail == "config" {
				wantCalls = 1
			}
			if fail == "sync" {
				wantCalls = 2
			}
			if count := len(strings.Split(strings.TrimSpace(string(calls)), "\n")); count != wantCalls {
				t.Fatalf("command continued after failure: %s", calls)
			}
		})
	}
}
