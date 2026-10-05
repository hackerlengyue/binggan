package surge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/capture"
)

func descriptor(t *testing.T) capture.Descriptor {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Isolated Surge test CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return capture.Descriptor{Version: 2, BackendExecutable: filepath.Join(t.TempDir(), "binggan-backend"), Ready: true, ProxyAddress: "127.0.0.1:18767", ProxyUsername: "sz-capture", ProxyPassword: strings.Repeat("a", 64), Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Fingerprint: hex.EncodeToString(sum[:])}
}

func TestProfilePatchPreservesExistingContents(t *testing.T) {
	for _, original := range []string{
		"[General]\nhttp-listen = 127.0.0.1:6152\n[Rule]\nFINAL,DIRECT\n",
		"[Proxy]\nExisting = direct\n[Rule]\nFINAL,Existing\n",
		"[Proxy]\r\nExisting = direct\r\n[Rule]\r\nFINAL,Existing",
		"[Proxy]",
	} {
		patched, block, err := patchProfile(original, descriptor(t))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(patched, "socks5, 127.0.0.1, 18767, sz-capture") {
			t.Fatal("missing authenticated local policy")
		}
		got, err := removeBlock(patched, block)
		if err != nil || got != original {
			t.Fatalf("profile was not restored exactly: %v", err)
		}
		got, err = removeBlock(patched+"\n# user edit\n", block)
		if err != nil || got != original+"\n# user edit\n" {
			t.Fatal("unrelated user edit was lost")
		}
		if _, err = removeBlock(strings.Replace(patched, "18767", "9999", 1), block); err == nil {
			t.Fatal("must not overwrite edits inside our block")
		}
	}
}

func TestRuleExcludesBackendAndCannotInjectRules(t *testing.T) {
	rule, err := captureRule("/Applications/饼干大小姐.app/Contents/MacOS/binggan")
	if err != nil || !strings.Contains(rule, "DOMAIN-SUFFIX,shenzaokeji.com") || !strings.Contains(rule, "NOT,((PROCESS-NAME,/Applications/饼干大小姐.app/Contents/MacOS/binggan))") {
		t.Fatalf("unsafe rule: %s, %v", rule, err)
	}
	for _, path := range []string{"relative", "/tmp/evil,FINAL,DIRECT", "/tmp/evil\n[Rule]", "/tmp/*.app", "/tmp/a(b)"} {
		if _, err := captureRule(path); err == nil {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
}

type fakeCLI struct {
	rules               []string
	profile, mode, fail string
	calls               []string
}

func (f *fakeCLI) run(_ context.Context, args ...string) ([]byte, error) {
	command := strings.Join(args, " ")
	f.calls = append(f.calls, command)
	if f.fail != "" && strings.HasPrefix(command, f.fail) {
		return nil, errors.New("simulated CLI failure")
	}
	var value any
	switch {
	case command == "--raw mode":
		value = map[string]any{"mode": f.mode}
	case command == "--raw profile current":
		value = map[string]any{"profile": f.profile}
	case command == "--raw rule temp list":
		value = map[string]any{"rules": f.rules}
	case strings.HasPrefix(command, "--raw rule match "):
		value = map[string]any{"policy": policyName}
	case strings.HasPrefix(command, "rule temp add "):
		// Surge canonicalizes logical rule separators in its list response.
		f.rules = append(f.rules, strings.ReplaceAll(args[3], "),(", "), ("))
	case strings.HasPrefix(command, "rule temp remove "):
		f.rules = slices.DeleteFunc(f.rules, func(s string) bool { return s == args[3] })
	case args[0] == "--check", command == "reload":
	default:
		return nil, errors.New("unexpected CLI command")
	}
	return json.Marshal(value)
}

func managerFixture(t *testing.T) (*Manager, *fakeCLI, string, string) {
	t.Helper()
	home := t.TempDir()
	profile := filepath.Join(home, "Library", "Application Support", "Surge", "Profiles", "User.conf")
	if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
		t.Fatal(err)
	}
	original := "[General]\nhttp-listen = 127.0.0.1:6152\n[Rule]\nFINAL,DIRECT\n"
	if err := os.WriteFile(profile, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCLI{profile: "User", mode: "rule", rules: []string{"DOMAIN,keep.example,DIRECT"}}
	return &Manager{home: home, dir: t.TempDir(), run: fake.run}, fake, profile, original
}

func TestEnableDisableAndCrashRecoveryKeepUserRules(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		m, cli, path, original := managerFixture(t)
		ctx := context.Background()
		if err := m.Enable(ctx, descriptor(t)); err != nil {
			t.Fatal(err)
		}
		if len(cli.rules) != 2 {
			t.Fatal("capture rule missing")
		}
		info, _ := os.Stat(filepath.Join(m.dir, "session.json"))
		if info.Mode().Perm() != 0600 {
			t.Fatal("credentials journal must be private")
		}
		b, _ := os.ReadFile(path)
		os.WriteFile(path, append(b, []byte("\n# later user edit\n")...), 0600)
		var err error
		if recovery {
			m = &Manager{home: m.home, dir: m.dir, run: cli.run}
			err = m.Recover(ctx)
		} else {
			err = m.Disable(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ = os.ReadFile(path)
		if string(b) != original+"\n# later user edit\n" {
			t.Fatal("did not restore original profile and later user edit")
		}
		if !slices.Equal(cli.rules, []string{"DOMAIN,keep.example,DIRECT"}) {
			t.Fatal("changed unrelated temporary rules")
		}
		if err = m.Disable(ctx); err != nil {
			t.Fatal("cleanup is not idempotent")
		}
	}
}

func TestFailedEnableRestoresProfileAndNeverChangesGlobalMode(t *testing.T) {
	for _, failure := range []string{"--check", "rule temp add"} {
		m, cli, path, original := managerFixture(t)
		cli.fail = failure
		if err := m.Enable(context.Background(), descriptor(t)); err == nil {
			t.Fatal("expected error")
		}
		b, _ := os.ReadFile(path)
		if string(b) != original || len(cli.rules) != 1 {
			t.Fatal("failed enable changed user settings")
		}
	}
	m, cli, path, original := managerFixture(t)
	cli.mode = "global"
	if err := m.Enable(context.Background(), descriptor(t)); err == nil {
		t.Fatal("must refuse global mode without changing it")
	}
	b, _ := os.ReadFile(path)
	if string(b) != original || cli.mode != "global" {
		t.Fatal("global mode changed")
	}
}

func TestDetectLostRuleAndChangedProfile(t *testing.T) {
	m, cli, _, _ := managerFixture(t)
	ctx := context.Background()
	if err := m.Enable(ctx, descriptor(t)); err != nil {
		t.Fatal(err)
	}
	cli.profile = "Other"
	if err := m.Check(ctx); err == nil {
		t.Fatal("profile switch went unnoticed")
	}
	cli.profile = "User"
	cli.rules = cli.rules[:1]
	if err := m.Check(ctx); err == nil {
		t.Fatal("lost capture rule went unnoticed")
	}
	if err := m.Disable(ctx); err != nil {
		t.Fatal("already removed rule must not prevent recovery", err)
	}
}

func TestFailedRuleRemovalKeepsPolicyUntilConfirmed(t *testing.T) {
	m, cli, path, original := managerFixture(t)
	ctx := context.Background()
	if err := m.Enable(ctx, descriptor(t)); err != nil {
		t.Fatal(err)
	}
	// Model a CLI that acknowledges removal without actually removing the rule.
	run := m.run
	m.run = func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.HasPrefix(strings.Join(args, " "), "rule temp remove ") {
			return []byte(`{}`), nil
		}
		return run(ctx, args...)
	}
	if err := m.Disable(ctx); err == nil {
		t.Fatal("must verify rule removal")
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), markerStart) || len(cli.rules) != 2 {
		t.Fatal("removed the policy while its rule still points to it")
	}
	m.run = run
	if err := m.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if string(b) != original {
		t.Fatal("retry failed to restore profile")
	}
}
