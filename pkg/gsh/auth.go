package gsh

import (
	"fmt"
	"io/ioutil"

	"gtools/pkg/auth"

	"golang.org/x/crypto/ssh"
)

type passwordAuthState struct {
	attempts int
}

func authenticationMethods(config Config) ([]ssh.AuthMethod, *passwordAuthState, error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	if config.IdentityFile != "" {
		signer, err := readPrivateKey(config)
		if err != nil {
			return nil, nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	state := &passwordAuthState{}
	passwordMethod := ssh.PasswordCallback(func() (string, error) {
		if state.attempts > 0 {
			fmt.Fprintln(config.Stderr, "Permission denied, please try again.")
		}
		state.attempts++
		password, err := readSecret(fmt.Sprintf("%s@%s's password: ", config.User, config.Host))
		if err != nil {
			return "", err
		}
		defer clearBytes(password)
		return string(password), nil
	})
	methods = append(methods, ssh.RetryableAuthMethod(passwordMethod, 3))
	return methods, state, nil
}

func readPrivateKey(config Config) (ssh.Signer, error) {
	contents, err := ioutil.ReadFile(config.IdentityFile)
	if err != nil {
		return nil, fmt.Errorf("read private key: %v", err)
	}
	defer clearBytes(contents)
	signer, err := ssh.ParsePrivateKey(contents)
	if err == nil {
		return signer, nil
	}
	if _, ok := err.(*ssh.PassphraseMissingError); !ok {
		return nil, fmt.Errorf("private key parsing failed: %v", err)
	}
	passphrase, readErr := readSecret("Private key passphrase: ")
	if readErr != nil {
		return nil, fmt.Errorf("read private key passphrase: %v", readErr)
	}
	defer clearBytes(passphrase)
	signer, err = ssh.ParsePrivateKeyWithPassphrase(contents, passphrase)
	if err != nil {
		return nil, fmt.Errorf("private key parsing failed: %v", err)
	}
	return signer, nil
}

func readSecret(prompt string) ([]byte, error) {
	return auth.ReadPassword(prompt)
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
