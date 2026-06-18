package netutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
)

type TLSOptions struct {
	Insecure bool
	CACert   string
	CertFile string
	KeyFile  string
}

func BuildTLSConfig(opt TLSOptions, resolve *ResolveInfo) (*tls.Config, error) {
	cfg := &tls.Config{
		InsecureSkipVerify: opt.Insecure,
		MinVersion:         tls.VersionTLS12,
	}

	if opt.CACert != "" {
		caPEM, err := ioutil.ReadFile(opt.CACert)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}

		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caPEM)
		cfg.RootCAs = pool
	}

	if opt.CertFile != "" || opt.KeyFile != "" {
		if opt.CertFile == "" || opt.KeyFile == "" {
			return nil, fmt.Errorf("cert/key must be used together")
		}

		cert, err := tls.LoadX509KeyPair(opt.CertFile, opt.KeyFile)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	if resolve != nil {
		cfg.ServerName = resolve.Host
	}

	return cfg, nil
}
