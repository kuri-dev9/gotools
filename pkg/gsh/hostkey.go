package gsh

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

func knownHostsCallback(config Config) (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	entries, err := readKnownHosts(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	hostToken := config.Host
	if config.Port != 22 {
		hostToken = "[" + config.Host + "]:" + strconv.Itoa(config.Port)
	}

	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		matchedHost := false
		for _, entry := range entries {
			if !entry.matches(hostToken) {
				continue
			}
			matchedHost = true
			if entry.key.Type() == key.Type() && bytes.Equal(entry.key.Marshal(), key.Marshal()) {
				if config.Verbose {
					fmt.Fprintf(config.Stderr, "gsh: verified %s host key %s\n", key.Type(), ssh.FingerprintSHA256(key))
				}
				return nil
			}
		}
		if matchedHost {
			return fmt.Errorf("host key mismatch for %s (received %s %s)", hostToken, key.Type(), ssh.FingerprintSHA256(key))
		}
		fmt.Fprintf(config.Stderr, "The authenticity of host %s cannot be established.\n%s key fingerprint is %s.\nContinue connecting (yes/no)? ", hostToken, key.Type(), ssh.FingerprintSHA256(key))
		answer, err := readAnswer(config.Stdin)
		if err != nil {
			return fmt.Errorf("read host key confirmation: %v", err)
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "yes" {
			return fmt.Errorf("host key was not accepted")
		}
		if err := appendKnownHost(path, hostToken, key); err != nil {
			return fmt.Errorf("save host key: %v", err)
		}
		return nil
	}, nil
}

type knownHostEntry struct {
	hosts []string
	key   ssh.PublicKey
}

func (entry knownHostEntry) matches(host string) bool {
	for _, candidate := range entry.hosts {
		if candidate == host {
			return true
		}
	}
	return false
}

func readKnownHosts(path string) ([]knownHostEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var entries []knownHostEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "|") || strings.HasPrefix(line, "@") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[1:], " ")))
		if err != nil {
			continue
		}
		entries = append(entries, knownHostEntry{hosts: strings.Split(fields[0], ","), key: key})
	}
	return entries, scanner.Err()
}

func readAnswer(reader io.Reader) (string, error) {
	answer, err := bufio.NewReader(reader).ReadString('\n')
	if err == io.EOF && answer != "" {
		return answer, nil
	}
	return answer, err
}

func appendKnownHost(path, host string, key ssh.PublicKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "%s %s", host, ssh.MarshalAuthorizedKey(key))
	return err
}
