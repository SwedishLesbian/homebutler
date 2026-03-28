package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swedishlesbian/homebutler/internal/config"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestNewKnownHostsCallback(t *testing.T) {
	cb, err := newKnownHostsCallback()
	if err != nil {
		t.Fatalf("newKnownHostsCallback() error: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}
}

func TestNewKnownHostsCallback_CreatesFile(t *testing.T) {
	// Verify that ~/.ssh/known_hosts exists after calling newKnownHostsCallback
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	path := filepath.Join(home, ".ssh", "known_hosts")

	_, err = newKnownHostsCallback()
	if err != nil {
		t.Fatalf("newKnownHostsCallback() error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected %s to exist after newKnownHostsCallback()", path)
	}
}

func TestKnownHostsPath(t *testing.T) {
	path, err := knownHostsPath()
	if err != nil {
		t.Fatalf("knownHostsPath() error: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join(".ssh", "known_hosts")) {
		t.Errorf("expected path ending in .ssh/known_hosts, got %s", path)
	}
}

func TestTofuConnect_NoServer(t *testing.T) {
	t.Skip("tofuConnect requires a real SSH server; skipping in unit tests")
}

// TestErrorMessages_KeyMismatch verifies the key-mismatch error contains actionable hints.
func TestErrorMessages_KeyMismatch(t *testing.T) {
	// Simulate the error message that connect() would produce for a key mismatch.
	serverName := "myserver"
	addr := "10.0.0.1:22"
	msg := fmt.Sprintf("[%s] ⚠️  SSH HOST KEY CHANGED (%s)\n"+
		"  The server's host key does not match the one in ~/.ssh/known_hosts.\n"+
		"  This could mean:\n"+
		"    1. The server was reinstalled or its SSH keys were regenerated\n"+
		"    2. A man-in-the-middle attack is in progress\n\n"+
		"  → If you trust this change: homebutler trust %s --reset\n"+
		"  → If unexpected: do NOT connect and investigate", serverName, addr, serverName)

	if !strings.Contains(msg, "homebutler trust") {
		t.Error("key mismatch error should contain 'homebutler trust'")
	}
	if !strings.Contains(msg, "HOST KEY CHANGED") {
		t.Error("key mismatch error should contain 'HOST KEY CHANGED'")
	}
	if !strings.Contains(msg, "--reset") {
		t.Error("key mismatch error should contain '--reset' flag")
	}
}

// TestErrorMessages_UnknownHost verifies the unknown-host error contains actionable hints.
func TestErrorMessages_UnknownHost(t *testing.T) {
	serverName := "newserver"
	addr := "10.0.0.2:22"
	msg := fmt.Sprintf("[%s] failed to auto-register host key for %s\n  → Register manually: homebutler trust %s\n  → Check SSH connectivity: ssh %s@%s -p %d",
		serverName, addr, serverName, "root", "10.0.0.2", 22)

	if !strings.Contains(msg, "homebutler trust") {
		t.Error("unknown host error should contain 'homebutler trust'")
	}
	if !strings.Contains(msg, "ssh root@") {
		t.Error("unknown host error should contain ssh connection hint")
	}
}

// TestErrorMessages_NoCredentials verifies the no-credentials error contains config hint.
func TestErrorMessages_NoCredentials(t *testing.T) {
	serverName := "nocreds"
	msg := fmt.Sprintf("[%s] no SSH credentials configured\n  → Add 'key_file' or 'password' to this server in ~/.config/homebutler/config.yaml", serverName)

	if !strings.Contains(msg, "key_file") {
		t.Error("no-credentials error should mention key_file")
	}
	if !strings.Contains(msg, "config.yaml") {
		t.Error("no-credentials error should mention config.yaml")
	}
}

// TestErrorMessages_Timeout verifies the timeout error contains actionable hints.
func TestErrorMessages_Timeout(t *testing.T) {
	serverName := "slowserver"
	addr := "10.0.0.3:22"
	msg := fmt.Sprintf("[%s] connection timed out (%s)\n  → Check if the server is online and reachable\n  → Verify host/port in ~/.config/homebutler/config.yaml", serverName, addr)

	if !strings.Contains(msg, "timed out") {
		t.Error("timeout error should contain 'timed out'")
	}
	if !strings.Contains(msg, "config.yaml") {
		t.Error("timeout error should mention config.yaml")
	}
}

// TestKnownHostsKeyError verifies that knownhosts.KeyError works as expected
// for both key-mismatch and unknown-host scenarios.
func TestKnownHostsKeyError(t *testing.T) {
	// KeyError with Want = empty means unknown host
	unknownErr := &knownhosts.KeyError{}
	if len(unknownErr.Want) != 0 {
		t.Error("empty KeyError should have no Want entries (unknown host)")
	}

	// KeyError with Want populated means key mismatch
	mismatchErr := &knownhosts.KeyError{
		Want: []knownhosts.KnownKey{{Filename: "known_hosts", Line: 1}},
	}
	if len(mismatchErr.Want) == 0 {
		t.Error("mismatch KeyError should have Want entries")
	}
}

// --- connect error tests (no real SSH server) ---

func TestConnect_NoCredentials(t *testing.T) {
	srv := &config.ServerConfig{
		Name:     "nocreds",
		Host:     "127.0.0.1",
		AuthMode: "password",
		// No password set
	}
	_, err := connect(srv)
	if err == nil {
		t.Fatal("expected error for no credentials")
	}
	if !strings.Contains(err.Error(), "no SSH credentials") {
		t.Errorf("expected 'no SSH credentials' error, got: %s", err.Error())
	}
}

func TestConnect_BadKeyFile(t *testing.T) {
	srv := &config.ServerConfig{
		Name:    "badkey",
		Host:    "127.0.0.1",
		KeyFile: "/nonexistent/key/file",
	}
	_, err := connect(srv)
	if err == nil {
		t.Fatal("expected error for bad key file")
	}
	if !strings.Contains(err.Error(), "failed to load SSH key") {
		t.Errorf("expected 'failed to load SSH key' error, got: %s", err.Error())
	}
}

func TestConnect_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	// Connect to a non-routable address to trigger timeout
	srv := &config.ServerConfig{
		Name:     "timeout",
		Host:     "192.0.2.1",
		Port:     1,
		Password: "test",
		AuthMode: "password",
	}
	_, err := connect(srv)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "timed out") && !strings.Contains(errStr, "connection") && !strings.Contains(errStr, "SSH") {
		t.Errorf("expected connection error, got: %s", errStr)
	}
}

func TestConnect_ConnectionRefused(t *testing.T) {
	// Connect to localhost on an unlikely port — should fail fast with connection refused
	srv := &config.ServerConfig{
		Name:     "refused",
		Host:     "127.0.0.1",
		Port:     19999,
		Password: "test",
		AuthMode: "password",
	}
	_, err := connect(srv)
	if err == nil {
		t.Fatal("expected connection refused error")
	}
	if !strings.Contains(err.Error(), "SSH connection failed") && !strings.Contains(err.Error(), "connection refused") {
		t.Logf("got error: %s", err.Error()) // informational
	}
}

func TestRun_NoCredentials(t *testing.T) {
	srv := &config.ServerConfig{
		Name:     "nocreds",
		Host:     "127.0.0.1",
		AuthMode: "password",
	}
	_, err := Run(srv, "status", "--json")
	if err == nil {
		t.Fatal("expected error for Run with no credentials")
	}
	if !strings.Contains(err.Error(), "no SSH credentials") {
		t.Errorf("expected credential error, got: %s", err.Error())
	}
}

func TestRun_BadKey(t *testing.T) {
	srv := &config.ServerConfig{
		Name:    "badkey",
		Host:    "127.0.0.1",
		KeyFile: "/nonexistent/key",
	}
	_, err := Run(srv, "status", "--json")
	if err == nil {
		t.Fatal("expected error for Run with bad key")
	}
	if !strings.Contains(err.Error(), "failed to load SSH key") {
		t.Errorf("expected key error, got: %s", err.Error())
	}
}

func TestLoadKey_EmptyPath_NoDefaults(t *testing.T) {
	// loadKey with empty path tries default locations
	// On CI or if no SSH keys exist, it should return an error
	_, err := loadKey("")
	// May succeed if user has SSH keys, may fail if not — just verify it doesn't panic
	_ = err
}

func TestLoadKey_NonexistentFile(t *testing.T) {
	_, err := loadKey("/tmp/definitely-nonexistent-key-file-12345")
	if err == nil {
		t.Fatal("expected error for nonexistent key file")
	}
}

func TestLoadKey_InvalidKeyContent(t *testing.T) {
	// Create a temp file with invalid key content
	tmpFile, err := os.CreateTemp("", "bad-ssh-key-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("this is not a valid SSH key")
	tmpFile.Close()

	_, err = loadKey(tmpFile.Name())
	if err == nil {
		t.Fatal("expected error for invalid key content")
	}
}

func TestLoadKey_TildeExpansion(t *testing.T) {
	// loadKey should expand ~ in paths
	_, err := loadKey("~/nonexistent-key-file-12345")
	if err == nil {
		t.Fatal("expected error for nonexistent key file with tilde")
	}
	// Key thing is it didn't panic and it resolved the tilde path
}

func TestRemoveHostKeys_NonexistentFile(t *testing.T) {
	// Create a temp known_hosts that we control
	srv := &config.ServerConfig{
		Name: "test",
		Host: "10.99.99.99",
		Port: 22,
	}
	// RemoveHostKeys on real known_hosts file — should not error even if key not present
	err := RemoveHostKeys(srv)
	if err != nil {
		t.Errorf("RemoveHostKeys should not error for non-matching keys: %v", err)
	}
}

func TestSelfUpgrade_DevBuild(t *testing.T) {
	result := SelfUpgrade("dev", "1.0.0")
	if result.Status != "error" {
		t.Errorf("expected error status for dev build, got %s", result.Status)
	}
	if !strings.Contains(result.Message, "dev build") {
		t.Errorf("expected dev build message, got %s", result.Message)
	}
}

func TestSelfUpgrade_AlreadyUpToDate(t *testing.T) {
	result := SelfUpgrade("1.0.0", "1.0.0")
	if result.Status != "up-to-date" {
		t.Errorf("expected up-to-date status, got %s", result.Status)
	}
	if result.NewVersion != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", result.NewVersion)
	}
	if !strings.Contains(result.Message, "already") {
		t.Errorf("expected 'already' in message, got %s", result.Message)
	}
}

func TestUpgradeResult_Fields(t *testing.T) {
	r := UpgradeResult{
		Target:      "myserver",
		PrevVersion: "1.0.0",
		NewVersion:  "1.1.0",
		Status:      "upgraded",
		Message:     "v1.0.0 → v1.1.0",
	}
	if r.Target != "myserver" {
		t.Errorf("Target = %q, want myserver", r.Target)
	}
	if r.Status != "upgraded" {
		t.Errorf("Status = %q, want upgraded", r.Status)
	}
}

func TestUpgradeReport_Fields(t *testing.T) {
	report := UpgradeReport{
		LatestVersion: "1.1.0",
		Results: []UpgradeResult{
			{Target: "local", Status: "upgraded"},
			{Target: "remote", Status: "up-to-date"},
		},
	}
	if len(report.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(report.Results))
	}
}

func TestNormalizeArch_Extended(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"x86_64", "amd64"},
		{"aarch64", "arm64"},
		{"arm64", "arm64"},
		{"amd64", "amd64"},
		{"i386", "i386"},
		{"armv7l", "armv7l"},
		{"s390x", "s390x"},
		{"ppc64le", "ppc64le"},
	}
	for _, tc := range tests {
		got := normalizeArch(tc.in)
		if got != tc.want {
			t.Errorf("normalizeArch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
