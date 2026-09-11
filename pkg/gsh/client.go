package gsh

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Config contains the local streams and SSH connection settings.
type Config struct {
	Host         string
	Port         int
	User         string
	IdentityFile string
	ForcePTY     bool
	Verbose      bool
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
}

// ExitError reports a remote command or shell exit status.
type ExitError struct {
	Status int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("remote session exited with status %d", e.Status)
}

// Run connects and starts either an interactive shell or a remote command.
func Run(config Config, command string) (int, error) {
	authMethods, passwordState, err := authenticationMethods(config)
	if err != nil {
		return 1, err
	}
	hostKeyCallback, err := knownHostsCallback(config)
	if err != nil {
		return 1, fmt.Errorf("host key verification setup failed: %v", err)
	}

	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	if config.Verbose {
		fmt.Fprintf(config.Stderr, "gsh: connecting to %s as %s\n", address, config.User)
		fmt.Fprintln(config.Stderr, "gsh: host key algorithms include ecdsa-sha2-nistp256")
	}
	clientConfig := &ssh.ClientConfig{
		User: config.User, Auth: authMethods, HostKeyCallback: hostKeyCallback,
		Timeout: 10 * time.Second,
	}
	client, err := ssh.Dial("tcp", address, clientConfig)
	if err != nil {
		if config.Verbose {
			fmt.Fprintf(config.Stderr, "gsh: SSH connection detail: %v\n", err)
		}
		return 1, classifyConnectError(config, passwordState, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return 1, fmt.Errorf("create SSH session: %v", err)
	}
	defer session.Close()
	session.Stdin = config.Stdin
	session.Stdout = config.Stdout
	session.Stderr = config.Stderr

	if command != "" {
		if config.Verbose {
			fmt.Fprintln(config.Stderr, "gsh: starting remote command")
		}
		if config.ForcePTY {
			return runWithPTY(session, config, command)
		}
		return waitStatus(session.Run(command))
	}
	return runWithPTY(session, config, "")
}

func waitStatus(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	if exitError, ok := err.(*ssh.ExitError); ok {
		status := exitError.ExitStatus()
		if status < 0 {
			return 1, fmt.Errorf("remote session ended without an exit status: %v", err)
		}
		return status, &ExitError{Status: status}
	}
	return 1, fmt.Errorf("remote session failed: %v", err)
}

func classifyConnectError(config Config, passwordState *passwordAuthState, err error) error {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "unable to authenticate"):
		if passwordState != nil && passwordState.attempts > 0 {
			return fmt.Errorf("%s@%s: Permission denied (password).", config.User, config.Host)
		}
		return fmt.Errorf("%s@%s: Permission denied.", config.User, config.Host)
	case strings.Contains(text, "no common algorithm") || strings.Contains(text, "no common algo"):
		return fmt.Errorf("no matching SSH algorithm")
	case strings.Contains(text, "host key"):
		detail := strings.TrimPrefix(err.Error(), "ssh: handshake failed: ")
		return fmt.Errorf("host key verification failed: %s", detail)
	case strings.Contains(text, "connection refused"):
		return fmt.Errorf("connect to %s:%d: Connection refused", config.Host, config.Port)
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline exceeded"):
		return fmt.Errorf("connect to %s:%d: Connection timed out", config.Host, config.Port)
	default:
		return fmt.Errorf("SSH connection to %s:%d failed", config.Host, config.Port)
	}
}
