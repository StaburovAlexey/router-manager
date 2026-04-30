package sshclient

import (
	"errors"
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
