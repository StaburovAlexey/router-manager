package setup

import (
	"bufio"
	"bytes"
	"errors"
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

func TestShouldReenterServerDetailsForHostKeyScanError(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("да\n"))
	var out bytes.Buffer
	err := hostKeyScanError{label: "входной сервер", err: errors.New("scan failed")}

	if !shouldReenterServerDetails(err, reader, &out, "входного сервера") {
		t.Fatal("expected yes answer to request re-entering server details")
	}
	for _, want := range []string{"Не удалось проверить SSH host key входного сервера", "Ввести данные входного сервера заново?"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output:\n%s", want, out.String())
		}
	}
}

func TestShouldReenterServerDetailsCanDecline(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("нет\n"))
	var out bytes.Buffer
	err := hostKeyScanError{label: "выходной сервер", err: errors.New("scan failed")}

	if shouldReenterServerDetails(err, reader, &out, "выходного сервера") {
		t.Fatal("expected no answer to decline re-entering server details")
	}
}

func TestShouldReenterServerDetailsIgnoresOtherErrors(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("да\n"))
	var out bytes.Buffer

	if shouldReenterServerDetails(errors.New("permission denied"), reader, &out, "входного сервера") {
		t.Fatal("non host key scan errors must not request server details again")
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output for ignored error:\n%s", out.String())
	}
}
