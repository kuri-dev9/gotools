package gsh

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func runWithPTY(session *ssh.Session, config Config, command string) (int, error) {
	input, ok := config.Stdin.(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return 1, fmt.Errorf("PTY allocation requires a local terminal")
	}
	width, height, err := term.GetSize(int(input.Fd()))
	if err != nil {
		return 1, fmt.Errorf("read terminal size: %v", err)
	}
	modes := ssh.TerminalModes{
		ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm", height, width, modes); err != nil {
		return 1, fmt.Errorf("PTY request failed: %v", err)
	}
	oldState, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return 1, fmt.Errorf("enter terminal raw mode: %v", err)
	}
	defer func() {
		if restoreErr := term.Restore(int(input.Fd()), oldState); restoreErr != nil {
			fmt.Fprintf(config.Stderr, "gsh: restore terminal: %v\n", restoreErr)
		}
	}()

	if config.Verbose {
		fmt.Fprintf(config.Stderr, "gsh: allocated remote PTY with %d rows and %d columns\n", height, width)
	}
	if command == "" {
		if err := session.Shell(); err != nil {
			return 1, fmt.Errorf("start remote shell: %v", err)
		}
	} else {
		if err := session.Start(command); err != nil {
			return 1, fmt.Errorf("start remote command: %v", err)
		}
	}
	stopResize := watchResize(session, input, config)
	defer stopResize()
	return waitStatus(session.Wait())
}

func watchResize(session *ssh.Session, input *os.File, config Config) func() {
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-signals:
				width, height, err := term.GetSize(int(input.Fd()))
				if err == nil {
					if err := session.WindowChange(height, width); err != nil && config.Verbose {
						fmt.Fprintf(config.Stderr, "gsh: resize remote terminal: %v\n", err)
					}
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}
