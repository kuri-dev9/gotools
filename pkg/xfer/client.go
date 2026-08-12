// Package xfer provides reusable file transfer clients.
package xfer

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	DefaultPort     = 22
	DefaultSFTPPort = 22
	DefaultFTPPort  = 21
)

// Protocol identifies a file transfer protocol.
type Protocol string

const (
	ProtocolSFTP Protocol = "sftp"
	ProtocolFTP  Protocol = "ftp"
)

// FTPMode identifies how an FTP data connection is established.
type FTPMode string

const (
	FTPModePassive FTPMode = "passive"
	FTPModeActive  FTPMode = "active"
)

// Config contains connection settings shared by transfer clients.
type Config struct {
	Protocol        Protocol
	Host            string
	Port            int
	Username        string
	Password        string
	Timeout         time.Duration
	HostKeyCallback ssh.HostKeyCallback
	FTPMode         FTPMode
}

// File describes a remote file.
type File struct {
	Name    string
	Path    string
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
}

// Result describes one completed or skipped transfer.
type Result struct {
	Source      string
	Destination string
	Skipped     bool
}

// DownloadMode controls source selection and existing destination handling.
type DownloadMode string

const (
	DownloadModeSkipWrite DownloadMode = "skip-write"
	DownloadModeOverwrite DownloadMode = "overwrite"
	DownloadModeError     DownloadMode = "error"
	DownloadModeOnce      DownloadMode = "once"
)

// ListOptions controls remote directory listing.
type ListOptions struct {
	Directory string
	Pattern   string
	Regex     bool
}

// DownloadOptions controls remote-to-local transfers.
type DownloadOptions struct {
	Remote         string
	LocalDir       string
	Pattern        string
	Regex          bool
	Rename         string
	RenameFunc     func(string) (string, error)
	Mode           DownloadMode
	OnceFile       string
	Overwrite      bool
	SkipExisting   bool
	RemoveRemote   bool
	CreateLocalDir bool
}

// UploadOptions controls local-to-remote transfers.
type UploadOptions struct {
	Local        string
	RemoteDir    string
	Pattern      string
	Regex        bool
	Rename       string
	RenameFunc   func(string) (string, error)
	Overwrite    bool
	SkipExisting bool
	RemoveLocal  bool
	CreateRemote bool
}

// RemoveOptions controls pattern-based remote deletion.
type RemoveOptions struct {
	Directory string
	Pattern   string
	Regex     bool
}

// Client is implemented by file transfer protocols.
type Client interface {
	Connect() error
	Close() error
	Check(remoteDir string) error
	List(options ListOptions) ([]File, error)
	Download(options DownloadOptions) ([]Result, error)
	Upload(options UploadOptions) ([]Result, error)
	Remove(options RemoveOptions) ([]string, error)
	Mkdir(remoteDir string) error
}

// New creates a transfer client for the configured protocol.
func New(config Config) (Client, error) {
	if config.Protocol == "" {
		config.Protocol = ProtocolSFTP
	}
	switch config.Protocol {
	case ProtocolSFTP:
		return NewSFTP(config)
	case ProtocolFTP:
		return NewFTP(config)
	default:
		return nil, fmt.Errorf("unsupported protocol %q", config.Protocol)
	}
}

// DefaultPortFor returns the default control port for a protocol.
func DefaultPortFor(protocol Protocol) (int, error) {
	switch protocol {
	case ProtocolSFTP:
		return DefaultSFTPPort, nil
	case ProtocolFTP:
		return DefaultFTPPort, nil
	default:
		return 0, fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func validateConfig(config Config) error {
	if config.Host == "" {
		return fmt.Errorf("host is required")
	}
	if config.Username == "" {
		return fmt.Errorf("username is required")
	}
	if config.Port < 0 || config.Port > 65535 {
		return fmt.Errorf("invalid port %d", config.Port)
	}
	if config.Timeout < 0 {
		return fmt.Errorf("timeout must not be negative")
	}
	return nil
}
