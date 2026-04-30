package sshclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplainHostKeyVerification(t *testing.T) {
	err := ExplainError(Target{User: "root", IP: "203.0.113.10", Port: 22}, errors.New("Host key verification failed."))
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	for _, want := range []string{"/root/.ssh/known_hosts", "ssh-keyscan", "sudo ssh -o BatchMode=yes", "203.0.113.10"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in %q", want, text)
		}
	}
}

func TestExplainPermissionDenied(t *testing.T) {
	err := ExplainError(Target{User: "root", IP: "203.0.113.10", Port: 22}, errors.New("Permission denied (publickey,password)."))
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	for _, want := range []string{"/root/.ssh", "id_ed25519", "ssh-copy-id", "authorized_keys", "sudo ssh -o BatchMode=yes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in %q", want, text)
		}
	}
}

func TestTrustHostKeyReplacesOldHostPortEntries(t *testing.T) {
	dir := t.TempDir()
	oldRootSSHDir := rootSSHDir
	rootSSHDir = dir
	t.Cleanup(func() { rootSSHDir = oldRootSSHDir })

	knownHosts := filepath.Join(dir, "known_hosts")
	oldText := "[203.0.113.10]:2222 ssh-ed25519 old-target\n198.51.100.1 ssh-ed25519 other-host\n"
	if err := os.WriteFile(knownHosts, []byte(oldText), 0o600); err != nil {
		t.Fatal(err)
	}
	hostKey := "[203.0.113.10]:2222 ssh-ed25519 new-target"
	target := Target{IP: "203.0.113.10", Port: 2222}

	if err := TrustHostKey(context.Background(), target, hostKey); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "old-target") {
		t.Fatalf("old target key was not removed:\n%s", text)
	}
	for _, want := range []string{"198.51.100.1 ssh-ed25519 other-host", hostKey} {
		if !strings.Contains(text, want) {
			t.Fatalf("known_hosts missing %q:\n%s", want, text)
		}
	}
}

func TestTrustHostKeyIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	oldRootSSHDir := rootSSHDir
	rootSSHDir = dir
	t.Cleanup(func() { rootSSHDir = oldRootSSHDir })

	hostKey := "203.0.113.10 ssh-ed25519 target-key"
	target := Target{IP: "203.0.113.10", Port: 22}
	for i := 0; i < 2; i++ {
		if err := TrustHostKey(context.Background(), target, hostKey); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), hostKey); got != 1 {
		t.Fatalf("host key count = %d, known_hosts:\n%s", got, string(data))
	}
}
