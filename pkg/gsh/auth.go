package gsh

import (
	"fmt"
	"io/ioutil"

	"gtools/pkg/auth"

	"golang.org/x/crypto/ssh"
)

func authenticationMethods(config Config) ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	if config.IdentityFile != "" {
		signer, err := readPrivateKey(config)
		if err != nil {
			return nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	methods = append(methods, ssh.PasswordCallback(func() (string, error) {
		password, err := readSecret(fmt.Sprintf("%s@%s's Password: ", config.User, config.Host))
		if err != nil {
			return "", err
		}
		defer clearBytes(password)
		return string(password), nil
	}))
	return methods, nil
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
