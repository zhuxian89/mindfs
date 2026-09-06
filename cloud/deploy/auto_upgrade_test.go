package deploy_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const upgradeRepository = "ghcr.io/zhuxian89/mindfs-relay"
const upgradeDigest = upgradeRepository + "@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const upgradeImageID = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// Only these commands may run. Any unrecognized command fails, including Git,
// Docker build/down and a run/up that ignores the pinned image or project.
const upgradeFakeCommand = `#!/usr/bin/env python3
import json, os, pathlib, sys
root = pathlib.Path(os.environ['UPGRADE_TEST_ROOT'])
args = sys.argv[1:]
program = pathlib.Path(sys.argv[0]).name
pinned = os.environ.get('RELAY_IMAGE', '')
project = os.environ.get('COMPOSE_PROJECT_NAME', '')
compose_file = os.environ.get('COMPOSE_FILE', '')
expected_file = os.environ.get('UPGRADE_TEST_EXPECTED_FILE', 'docker-compose.1panel.yml')
digest = os.environ['UPGRADE_TEST_DIGEST']
image_id = os.environ['UPGRADE_TEST_IMAGE_ID']
scenario = os.environ.get('UPGRADE_TEST_SCENARIO', '')

def count(name):
    path = root / (name + '-count')
    value = int(path.read_text()) + 1 if path.exists() else 1
    path.write_text(str(value))
    return value

def event(stage):
    with (root / 'calls').open('a') as f:
        f.write(json.dumps({'stage': stage, 'image': pinned, 'project': project, 'file': compose_file}) + '\n')
    if os.environ.get('UPGRADE_TEST_FAIL') == stage:
        print('injected ' + stage + ' failure', file=sys.stderr)
        sys.exit(17)

if program == 'curl':
    url = args[-1]
    stage = url.rsplit('/', 1)[-1] if '/mindfs-assets/' not in url else 'asset'
    event(stage)
    if not url.startswith('https://relay.example.invalid/'):
        sys.exit(91)
    if stage == 'asset':
        print('404' if scenario == 'asset-404' else '200', end='')
    else:
        print(json.dumps({'status': 'wrong' if scenario == 'bad-health-body' else ('ok' if stage == 'healthz' else 'ready')}))
    sys.exit(0)

if args == ['compose', 'config', '--format', 'json']:
    event('config' if count('config') == 1 else 'pinned-config')
    c = json.loads((root / 'config.json').read_text())
    for service in ('relay', 'asset-sync'):
        c['services'][service]['image'] = pinned or os.environ.get('UPGRADE_TEST_REQUEST', '') or 'ghcr.io/zhuxian89/mindfs-relay:latest'
    if scenario == 'hardcoded-image':
        for service in ('relay', 'asset-sync'):
            c['services'][service]['image'] = 'ghcr.io/zhuxian89/mindfs-relay:latest'
    if scenario == 'split-assets':
        c['services']['asset-sync']['volumes'][0]['source'] = 'other-assets'
    if scenario == 'build':
        c['services']['relay']['build'] = {'context': '..'}
    print(json.dumps(c))
elif args == ['inspect', 'mindfs-relay']:
    event('post-identity' if count('identity') == 3 else 'identity')
    c = json.loads((root / 'container.json').read_text())
    if scenario == 'wrong-project':
        c[0]['Config']['Labels']['com.docker.compose.project'] = 'different'
    if scenario == 'wrong-service':
        c[0]['Config']['Labels']['com.docker.compose.service'] = 'different'
    if scenario in ('wrong-volume', 'bind-volume', 'wrong-readonly'):
        mount = c[0]['Mounts'][0]
        if scenario == 'wrong-volume': mount['Name'] = 'empty-new-volume'
        if scenario == 'bind-volume': mount['Type'] = 'bind'
        if scenario == 'wrong-readonly': mount['RW'] = False
    print(json.dumps(c))
elif len(args) == 4 and args[:2] == ['inspect', '--format'] and args[-1] == 'mindfs-relay':
    event('running')
    print((root / 'running').read_text().strip())
elif len(args) == 3 and args[:2] == ['pull', '--quiet']:
    event('pull')
elif len(args) == 3 and args[:2] == ['image', 'inspect']:
    event('image')
    print(json.dumps([{'Id': image_id, 'RepoDigests': [] if scenario == 'no-digest' else [digest]}]))
elif args == ['image', 'inspect', '--format', '{{.Id}}', digest]:
    event('pinned')
    print('sha256:wrong' if scenario == 'wrong-pinned-id' else image_id)
elif args == ['compose', 'config', '--quiet']:
    event('refresh-config')
elif len(args) == 7 and args[:6] == ['compose', 'exec', '-T', 'relay', 'mindfs-relay', 'backup']:
    if not args[-1].startswith('/backups/mindfs-cloud-pre-upgrade-'): sys.exit(92)
    event('backup')
elif args[:4] == ['compose', 'run', '--rm', '--no-deps']:
    commands = {('asset-sync',): 'sync', ('relay', 'check-assets', '/var/lib/mindfs-assets'): 'check', ('relay', 'validate'): 'validate', ('relay', 'migrate'): 'migrate'}
    stage = commands.get(tuple(args[4:]))
    if not stage or pinned != digest or project != 'existing' or compose_file != expected_file: sys.exit(93)
    event(stage)
elif args == ['compose', 'up', '-d', '--force-recreate', '--no-deps', '--no-build', '--pull', 'never', '--wait', '--wait-timeout', '120', 'relay']:
    if pinned != digest or project != 'existing': sys.exit(94)
    event('up')
    (root / 'running').write_text(image_id + ' running ' + ('unhealthy' if scenario == 'unhealthy-after-up' else 'healthy'))
elif len(args) == 4 and args[:3] == ['compose', 'cp', 'relay:/var/lib/mindfs-assets/index.html']:
    event('cp')
    pathlib.Path(args[3]).write_text('<html></html>' if scenario == 'empty-index' else '<script src="/assets/index-new.js"></script><link href="./assets/index-new.css">')
else:
    print('unexpected command: ' + program + ' ' + ' '.join(args), file=sys.stderr)
    sys.exit(99)
`

type upgradeFixture struct{ root, deploy string }

func newUpgradeFixture(t *testing.T) upgradeFixture {
	t.Helper()
	f := upgradeFixture{root: t.TempDir()}
	f.deploy = filepath.Join(f.root, "checkout", "cloud", "deploy")
	bin := filepath.Join(f.root, "bin")
	for _, dir := range []string{f.deploy, bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, script := range []string{"auto-upgrade.sh", "refresh-assets.sh", "sync-upstream.sh", "auto-upgrade-cron.sh"} {
		writeExecutable(t, filepath.Join(f.deploy, script), read(t, script))
	}
	for _, command := range []string{"docker", "curl"} {
		writeExecutable(t, filepath.Join(bin, command), upgradeFakeCommand)
	}
	writeExecutable(t, filepath.Join(bin, "git"), "#!/bin/sh\necho 'unexpected Git call' >&2\nexit 99\n")
	maintenanceTestPath(t, bin)
	t.Setenv("UPGRADE_TEST_ROOT", f.root)
	t.Setenv("UPGRADE_TEST_DIGEST", upgradeDigest)
	t.Setenv("UPGRADE_TEST_IMAGE_ID", upgradeImageID)
	t.Setenv("UPGRADE_TEST_FAIL", "")
	t.Setenv("UPGRADE_TEST_SCENARIO", "")
	t.Setenv("UPGRADE_TEST_REQUEST", "")
	t.Setenv("UPGRADE_TEST_EXPECTED_FILE", "docker-compose.1panel.yml")
	t.Setenv("COMPOSE_FILE", "")
	t.Setenv("COMPOSE_PROJECT_NAME", "")
	t.Setenv("RELAY_IMAGE", "")
	f.write(t, "running", "sha256:old running healthy")
	f.write(t, "config.json", `{"name":"existing","services":{"relay":{"container_name":"mindfs-relay","environment":{"MINDFS_CLOUD_PUBLIC_URL":"https://relay.example.invalid","MINDFS_CLOUD_TOKEN_KEY":"DO-NOT-LOG-THIS-SECRET"},"volumes":[{"type":"volume","source":"relay-data","target":"/var/lib/mindfs-cloud"},{"type":"volume","source":"relay-assets","target":"/var/lib/mindfs-assets","read_only":true},{"type":"volume","source":"relay-backups","target":"/backups"}]},"asset-sync":{"volumes":[{"type":"volume","source":"relay-assets","target":"/var/lib/mindfs-assets"}]}},"volumes":{"relay-data":{"name":"existing_relay-data"},"relay-assets":{"name":"existing_relay-assets"},"relay-backups":{"name":"existing_relay-backups"}}}`)
	f.write(t, "container.json", `[{"Config":{"Labels":{"com.docker.compose.project":"existing","com.docker.compose.service":"relay"}},"State":{"Status":"running"},"Mounts":[{"Type":"volume","Name":"existing_relay-data","Destination":"/var/lib/mindfs-cloud","RW":true},{"Type":"volume","Name":"existing_relay-assets","Destination":"/var/lib/mindfs-assets","RW":false},{"Type":"volume","Name":"existing_relay-backups","Destination":"/backups","RW":true}]}]`)
	return f
}

func (f upgradeFixture) write(t *testing.T, file, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, file), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f upgradeFixture) marker(t *testing.T, value string) string {
	t.Helper()
	dir := filepath.Join(f.deploy, ".relay-upgrade")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "successful-image")
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f upgradeFixture) run(t *testing.T, failure bool) string {
	t.Helper()
	for _, name := range []string{"calls", "config-count", "identity-count"} {
		if err := os.Remove(filepath.Join(f.root, name)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", filepath.Join(f.deploy, "auto-upgrade.sh"))
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	if (err != nil) != failure {
		t.Fatalf("upgrade error=%v, want failure=%v\n%s", err, failure, out)
	}
	if strings.Contains(string(out), "DO-NOT-LOG-THIS-SECRET") {
		t.Fatal("Compose secrets were logged")
	}
	entries, err := filepath.Glob(filepath.Join(f.deploy, ".relay-upgrade", "check.*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("private verification files were not cleaned: %v %v", entries, err)
	}
	return string(out)
}

func (f upgradeFixture) stages(t *testing.T) []string {
	t.Helper()
	var stages []string
	for _, line := range strings.Split(strings.TrimSpace(read(t, filepath.Join(f.root, "calls"))), "\n") {
		var call struct{ Stage, Image, Project, File string }
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		stages = append(stages, call.Stage)
	}
	return stages
}

func TestAutoUpgradePinsImageAndSkipsOnlyVerifiedSuccess(t *testing.T) {
	f := newUpgradeFixture(t)
	marker := f.marker(t, "previous-digest")
	out := f.run(t, false)
	if !strings.Contains(out, "UPGRADE OK") || strings.TrimSpace(read(t, marker)) != upgradeDigest {
		t.Fatalf("successful version was not recorded: %s", out)
	}
	want := "config identity pull image pinned pinned-config identity backup refresh-config sync check validate migrate up post-identity running healthz readyz cp asset asset"
	if got := strings.Join(f.stages(t), " "); got != want {
		t.Fatalf("incorrect upgrade sequence:\n%s\nwant:\n%s", got, want)
	}
	if out := f.run(t, false); !strings.HasPrefix(out, "UPGRADE SKIPPED:") {
		t.Fatal(out)
	}
	if strings.Contains(strings.Join(f.stages(t), " "), "backup") {
		t.Fatal("healthy successful image was redeployed")
	}
	// Same marker with an old or unhealthy actual container must not skip.
	for _, current := range []string{"sha256:old running healthy", upgradeImageID + " running unhealthy"} {
		f.write(t, "running", current)
		if out := f.run(t, false); !strings.Contains(out, "UPGRADE OK") {
			t.Fatal(out)
		}
	}
}

func TestAutoUpgradeFailuresKeepSuccessMarkerAndRetry(t *testing.T) {
	for _, stage := range []string{"config", "identity", "pull", "image", "pinned", "pinned-config", "backup", "refresh-config", "sync", "check", "validate", "migrate", "up", "post-identity", "running", "healthz", "readyz", "cp", "asset"} {
		t.Run(stage, func(t *testing.T) {
			f := newUpgradeFixture(t)
			marker := f.marker(t, "previous-digest")
			t.Setenv("UPGRADE_TEST_FAIL", stage)
			f.run(t, true)
			if strings.TrimSpace(read(t, marker)) != "previous-digest" {
				t.Fatal("failure overwrote successful version")
			}
			stages := f.stages(t)
			if stages[len(stages)-1] != stage {
				t.Fatalf("continued after failure: %v", stages)
			}
			t.Setenv("UPGRADE_TEST_FAIL", "")
			if out := f.run(t, false); !strings.Contains(out, "UPGRADE OK") {
				t.Fatalf("failed version was not retried: %s", out)
			}
		})
	}
}

func TestAutoUpgradeRejectsIdentityAndVerificationMismatch(t *testing.T) {
	for _, scenario := range []string{"wrong-project", "wrong-service", "wrong-volume", "bind-volume", "wrong-readonly", "split-assets", "build", "hardcoded-image", "no-digest", "wrong-pinned-id", "unhealthy-after-up", "bad-health-body", "empty-index", "asset-404"} {
		t.Run(scenario, func(t *testing.T) {
			f := newUpgradeFixture(t)
			marker := f.marker(t, "previous-digest")
			t.Setenv("UPGRADE_TEST_SCENARIO", scenario)
			f.run(t, true)
			if strings.TrimSpace(read(t, marker)) != "previous-digest" {
				t.Fatal("mismatch recorded successful deployment")
			}
			if !strings.Contains(scenario, "after-up") && scenario != "bad-health-body" && scenario != "empty-index" && scenario != "asset-404" {
				if strings.Contains(strings.Join(f.stages(t), " "), "backup") {
					t.Fatal("identity/image mismatch reached mutation stages")
				}
			}
		})
	}
}

func TestAutoUpgradeAcceptsExplicitDigest(t *testing.T) {
	f := newUpgradeFixture(t)
	t.Setenv("UPGRADE_TEST_REQUEST", upgradeDigest)
	f.run(t, false)
}

func TestAutoUpgradePreservesCallerComposeFiles(t *testing.T) {
	f := newUpgradeFixture(t)
	files := "/server/existing stack/compose.yml:/server/override.yml"
	t.Setenv("COMPOSE_FILE", files)
	t.Setenv("UPGRADE_TEST_EXPECTED_FILE", files)
	t.Setenv("COMPOSE_PROJECT_NAME", "existing")
	f.run(t, false)
}

func TestMaintenanceLockOutcomes(t *testing.T) {
	for _, code := range []string{"75", "1"} {
		t.Run(code, func(t *testing.T) {
			f := newUpgradeFixture(t)
			writeExecutable(t, filepath.Join(f.root, "bin", "flock"), "#!/bin/sh\nexit "+code+"\n")
			writeExecutable(t, filepath.Join(f.root, "bin", "git"), "#!/bin/sh\n[ \"$*\" = 'rev-parse --show-toplevel' ] || exit 99\nprintf '%s\\n' \"$UPGRADE_TEST_ROOT/checkout\"\n")
			for _, script := range []string{"auto-upgrade.sh", "sync-upstream.sh"} {
				cmd := exec.Command("bash", filepath.Join(f.deploy, script))
				out, err := cmd.CombinedOutput()
				if (err != nil) != (code == "1") || (strings.Contains(string(out), "SKIPPED:") != (code == "75")) {
					t.Fatalf("incorrect lock outcome: %v %s", err, out)
				}
			}
			if _, err := os.Stat(filepath.Join(f.root, "calls")); !os.IsNotExist(err) {
				t.Fatal("contended or failed lock reached Docker")
			}
		})
	}
}

func TestCronWrapperPreservesFailuresContainingSkipped(t *testing.T) {
	for _, tc := range []struct {
		output string
		code   int
		quiet  bool
	}{
		{"UPGRADE SKIPPED: healthy", 0, true},
		{"UPGRADE OK: done", 0, false},
		{"UPGRADE SKIPPED: earlier\nUPGRADE FAILED: later", 17, false},
		{"diagnostic contains UPGRADE SKIPPED", 0, false},
	} {
		t.Run(tc.output, func(t *testing.T) {
			dir := t.TempDir()
			writeExecutable(t, filepath.Join(dir, "auto-upgrade-cron.sh"), read(t, "auto-upgrade-cron.sh"))
			writeExecutable(t, filepath.Join(dir, "auto-upgrade.sh"), "#!/bin/sh\nprintf '%s\\n' \"$CRON_TEST_OUTPUT\"\nexit \"$CRON_TEST_CODE\"\n")
			t.Setenv("CRON_TEST_OUTPUT", tc.output)
			code, _ := json.Marshal(tc.code)
			t.Setenv("CRON_TEST_CODE", string(code))
			cmd := exec.Command("bash", filepath.Join(dir, "auto-upgrade-cron.sh"))
			out, err := cmd.CombinedOutput()
			if (err != nil) != (tc.code != 0) || cmd.ProcessState.ExitCode() != tc.code {
				t.Fatalf("exit code lost: %v %s", err, out)
			}
			if (len(out) == 0) != tc.quiet || (!tc.quiet && strings.TrimSpace(string(out)) != tc.output) {
				t.Fatalf("incorrect wrapper output: %q", out)
			}
		})
	}
}

func TestMaintenanceScriptsShareRealFlock(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real util-linux flock contention runs on Linux CI")
	}
	f := newUpgradeFixture(t)
	f.marker(t, "previous-digest")
	lock := filepath.Join(f.deploy, ".relay-upgrade", "operation.lock")
	ready := filepath.Join(f.root, "locked")
	holder := exec.Command("flock", lock, "sh", "-c", `touch "$1"; read -r line`, "sh", ready)
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = holder.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock holder did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, script := range []string{"auto-upgrade.sh", "sync-upstream.sh"} {
		// sync resolves its checkout before acquiring the lock.
		writeExecutable(t, filepath.Join(f.root, "bin", "git"), "#!/bin/sh\n[ \"$*\" = 'rev-parse --show-toplevel' ] || exit 99\nprintf '%s\\n' \"$UPGRADE_TEST_ROOT/checkout\"\n")
		cmd := exec.Command("bash", filepath.Join(f.deploy, script))
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "SKIPPED:") {
			t.Fatalf("lock contention was not skipped: %v %s", err, out)
		}
	}
}
