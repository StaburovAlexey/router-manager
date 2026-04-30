package confirm

import (
	"bufio"
	"strings"
	"testing"
)

func TestAskYesNoReturnsTrueForYes(t *testing.T) {
	var out strings.Builder
	if !AskYesNo(bufio.NewReader(strings.NewReader("yes\n")), &out, "Продолжить") {
		t.Fatal("yes must confirm")
	}
}

func TestAskYesNoReturnsFalseForNo(t *testing.T) {
	var out strings.Builder
	if AskYesNo(bufio.NewReader(strings.NewReader("no\n")), &out, "Продолжить") {
		t.Fatal("no must cancel")
	}
}

func TestAskYesNoRequiresExplicitAnswer(t *testing.T) {
	var out strings.Builder
	if !AskYesNo(bufio.NewReader(strings.NewReader("\nwrong\nyes\n")), &out, "Продолжить") {
		t.Fatal("yes after invalid answers must confirm")
	}
	text := out.String()
	if strings.Count(text, RequiredMessage) != 2 {
		t.Fatalf("invalid answers must print required message twice:\n%s", text)
	}
}

func TestAskYesNoRejectsInvalidBeforeNo(t *testing.T) {
	var out strings.Builder
	if AskYesNo(bufio.NewReader(strings.NewReader("maybe\nno\n")), &out, "Продолжить") {
		t.Fatal("no after invalid answer must cancel")
	}
	if !strings.Contains(out.String(), RequiredMessage) {
		t.Fatalf("invalid answer must print required message:\n%s", out.String())
	}
}
