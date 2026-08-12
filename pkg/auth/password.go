package auth

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// ReadPassword reads one line from the terminal with echo disabled. The prompt
// is written to stderr so command output remains safe for shell substitution.
func ReadPassword(prompt string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if _, err := fmt.Fprint(os.Stderr, prompt); err != nil {
		return nil, err
	}
	password, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		clearBytes(password)
		return nil, err
	}
	return password, nil
}
