package confirm

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const RequiredMessage = "Введите да/нет или yes/no."

func ParseYesNo(text string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "да", "д", "yes", "y":
		return true, true
	case "нет", "н", "no", "n":
		return false, true
	default:
		return false, false
	}
}

func AskYesNo(reader *bufio.Reader, out io.Writer, label string) bool {
	for {
		fmt.Fprintf(out, "%s (да/нет): ", label)
		text, err := reader.ReadString('\n')
		if answer, ok := ParseYesNo(text); ok {
			return answer
		}
		if strings.TrimSpace(text) == "" && err != nil {
			return false
		}
		if strings.TrimSpace(text) == "" {
			fmt.Fprintln(out, RequiredMessage)
			continue
		}
		if err == nil {
			fmt.Fprintln(out, RequiredMessage)
			continue
		}
		return false
	}
}
