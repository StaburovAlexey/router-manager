package confirm

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const RequiredMessage = "Нужно обязательно ввести yes или no."

func AskYesNo(reader *bufio.Reader, out io.Writer, label string) bool {
	for {
		fmt.Fprintf(out, "%s (yes/no): ", label)
		text, err := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(text))
		switch answer {
		case "yes":
			return true
		case "no":
			return false
		default:
			fmt.Fprintln(out, RequiredMessage)
			if err != nil && strings.TrimSpace(text) == "" {
				return false
			}
		}
	}
}
