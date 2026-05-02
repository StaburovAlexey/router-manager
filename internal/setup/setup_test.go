package setup

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestAskRequiredRepeatsWhenValueAndDefaultAreEmpty(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n203.0.113.10\n"))
	var out bytes.Buffer

	value := askRequired(reader, &out, "IP входного сервера", "")

	if value != "203.0.113.10" {
		t.Fatalf("askRequired() = %q, want %q", value, "203.0.113.10")
	}
	if got := strings.Count(out.String(), "IP входного сервера"); got < 3 {
		t.Fatalf("expected prompt, validation message, and retry prompt in output, got %d occurrences:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "IP входного сервера не может быть пустым.") {
		t.Fatalf("missing required message in output:\n%s", out.String())
	}
}

func TestAskRequiredKeepsDefaultOnEmptyInput(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))
	var out bytes.Buffer

	value := askRequired(reader, &out, "IP входного сервера", "203.0.113.10")

	if value != "203.0.113.10" {
		t.Fatalf("askRequired() = %q, want default", value)
	}
	if strings.Contains(out.String(), "не может быть пустым") {
		t.Fatalf("default should not trigger validation message:\n%s", out.String())
	}
}

func TestAskRequiredReturnsEnteredValue(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("198.51.100.20\n"))
	var out bytes.Buffer

	value := askRequired(reader, &out, "IP выходного сервера", "")

	if value != "198.51.100.20" {
		t.Fatalf("askRequired() = %q, want entered value", value)
	}
}
