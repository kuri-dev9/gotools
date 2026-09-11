package gsh

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
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
	path := filepath.Join(home, ".gsh", "known_hosts")
	entries, err := readKnownHosts(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		if err := os.Chmod(path, 0600); err != nil {
			return nil, err
		}
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
			fmt.Fprintf(config.Stderr, "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED for %s!\n", hostToken)
			fmt.Fprintf(config.Stderr, "The new %s key fingerprint is %s.\n", key.Type(), ssh.FingerprintSHA256(key))
			fmt.Fprint(config.Stderr, "Are you sure you want to continue connecting (yes/no)? ")
			answer, err := readAnswer(config.Stdin)
			if err != nil {
				return fmt.Errorf("read host key confirmation: %v", err)
			}
			if strings.ToLower(strings.TrimSpace(answer)) != "yes" {
				return fmt.Errorf("host key was not accepted")
			}
			if err := replaceKnownHost(path, hostToken, key); err != nil {
				return fmt.Errorf("replace host key: %v", err)
			}
			return nil
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
	if err := ensureKnownHostsDirectory(path); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	if info.Size() > 0 {
		last := make([]byte, 1)
		if _, err := file.ReadAt(last, info.Size()-1); err != nil {
			file.Close()
			return err
		}
		if last[0] != '\n' {
			if _, err := file.Write([]byte("\n")); err != nil {
				file.Close()
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(file, "%s %s", host, ssh.MarshalAuthorizedKey(key)); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func ensureKnownHostsDirectory(path string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	return os.Chmod(directory, 0700)
}

func replaceKnownHost(path, host string, key ssh.PublicKey) error {
	if err := ensureKnownHostsDirectory(path); err != nil {
		return err
	}
	contents, err := ioutil.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var output bytes.Buffer
	for len(contents) > 0 {
		line := contents
		if index := bytes.IndexByte(contents, '\n'); index >= 0 {
			line = contents[:index+1]
			contents = contents[index+1:]
		} else {
			contents = nil
		}
		output.Write(removeHostFromLine(line, host))
	}
	if output.Len() > 0 {
		current := output.Bytes()
		if current[len(current)-1] != '\n' {
			output.WriteByte('\n')
		}
	}
	fmt.Fprintf(&output, "%s %s", host, ssh.MarshalAuthorizedKey(key))

	temporary, err := ioutil.TempFile(filepath.Dir(path), ".known_hosts-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(output.Bytes()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	keepTemporary = false
	return nil
}

func removeHostFromLine(line []byte, host string) []byte {
	ending := ""
	body := line
	if len(body) > 0 && body[len(body)-1] == '\n' {
		ending = "\n"
		body = body[:len(body)-1]
		if len(body) > 0 && body[len(body)-1] == '\r' {
			ending = "\r\n"
			body = body[:len(body)-1]
		}
	}
	fields := strings.Fields(string(body))
	if len(fields) < 3 || strings.HasPrefix(fields[0], "#") ||
		strings.HasPrefix(fields[0], "|") || strings.HasPrefix(fields[0], "@") {
		return line
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[1:], " "))); err != nil {
		return line
	}
	hosts := strings.Split(fields[0], ",")
	remaining := hosts[:0]
	found := false
	for _, candidate := range hosts {
		if candidate == host {
			found = true
			continue
		}
		remaining = append(remaining, candidate)
	}
	if !found {
		return line
	}
	if len(remaining) == 0 {
		return nil
	}
	return []byte(strings.Join(remaining, ",") + " " + strings.Join(fields[1:], " ") + ending)
}
