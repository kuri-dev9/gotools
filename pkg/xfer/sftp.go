package xfer

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPClient implements Client using SSH File Transfer Protocol.
type SFTPClient struct {
	config Config
	conn   net.Conn
	ssh    *ssh.Client
	sftp   *sftp.Client
}

// NewSFTP creates a disconnected SFTP client.
func NewSFTP(config Config) (*SFTPClient, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.Port == 0 {
		config.Port = DefaultSFTPPort
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.HostKeyCallback == nil {
		// Compatibility with Paramiko AutoAddPolicy. Callers should provide a
		// known-host callback when strict host verification is required.
		config.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	}
	return &SFTPClient{config: config}, nil
}

// Connect establishes SSH and SFTP sessions.
func (c *SFTPClient) Connect() error {
	if c.sftp != nil {
		return nil
	}
	sshConfig := &ssh.ClientConfig{
		User:            c.config.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(c.config.Password)},
		HostKeyCallback: c.config.HostKeyCallback,
		Timeout:         c.config.Timeout,
	}
	address := net.JoinHostPort(c.config.Host, fmt.Sprint(c.config.Port))
	connection, err := net.DialTimeout("tcp", address, c.config.Timeout)
	if err != nil {
		return fmt.Errorf("connect SFTP: %w", err)
	}
	sshConnection, channels, requests, err := ssh.NewClientConn(connection, address, sshConfig)
	if err != nil {
		connection.Close()
		return fmt.Errorf("start SSH session: %w", err)
	}
	sshClient := ssh.NewClient(sshConnection, channels, requests)
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return fmt.Errorf("start SFTP session: %w", err)
	}
	c.conn = connection
	c.ssh = sshClient
	c.sftp = sftpClient
	return nil
}

// Close closes SFTP and SSH sessions.
func (c *SFTPClient) Close() error {
	var first error
	if c.sftp != nil {
		if err := c.sftp.Close(); err != nil {
			first = err
		}
		c.sftp = nil
	}
	if c.ssh != nil {
		if err := c.ssh.Close(); err != nil && first == nil {
			first = err
		}
		c.ssh = nil
	}
	c.conn = nil
	return first
}

// Check verifies login, remote directory stat, and listing.
func (c *SFTPClient) Check(remoteDir string) error {
	if err := c.connected(); err != nil {
		return err
	}
	if remoteDir == "" {
		remoteDir = "."
	}
	if c.conn != nil {
		if err := c.conn.SetDeadline(time.Now().Add(c.config.Timeout)); err != nil {
			return fmt.Errorf("set check deadline: %w", err)
		}
		defer c.conn.SetDeadline(time.Time{})
	}
	info, err := c.sftp.Stat(remoteDir)
	if err != nil {
		return fmt.Errorf("stat remote directory %q: %w", remoteDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("remote path %q is not a directory", remoteDir)
	}
	if _, err := c.sftp.ReadDir(remoteDir); err != nil {
		return fmt.Errorf("list remote directory %q: %w", remoteDir, err)
	}
	return nil
}

// List returns matching regular files and directories in a remote directory.
func (c *SFTPClient) List(options ListOptions) ([]File, error) {
	if err := c.connected(); err != nil {
		return nil, err
	}
	if options.Directory == "" {
		options.Directory = "."
	}
	m, err := newMatcher(options.Pattern, options.Regex)
	if err != nil {
		return nil, err
	}
	entries, err := c.sftp.ReadDir(options.Directory)
	if err != nil {
		return nil, fmt.Errorf("list %q: %w", options.Directory, err)
	}
	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		if !m.match(entry.Name()) {
			continue
		}
		files = append(files, File{
			Name: entry.Name(), Path: path.Join(options.Directory, entry.Name()),
			Size: entry.Size(), Mode: entry.Mode(), ModTime: entry.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// Download transfers matching remote files through local temporary files.
func (c *SFTPClient) Download(options DownloadOptions) ([]Result, error) {
	if err := c.connected(); err != nil {
		return nil, err
	}
	if options.LocalDir == "" {
		options.LocalDir = "."
	}
	if options.Overwrite && options.SkipExisting {
		return nil, fmt.Errorf("overwrite and skip-existing cannot be used together")
	}
	if options.CreateLocalDir {
		if err := os.MkdirAll(options.LocalDir, 0755); err != nil {
			return nil, fmt.Errorf("create local directory: %w", err)
		}
	}
	remoteFiles, err := c.remoteFiles(options.Remote, options.Pattern, options.Regex)
	if err != nil {
		return nil, err
	}
	remoteFiles, overwrite, skipExisting, err := prepareDownloadFiles(remoteFiles, options)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(remoteFiles))
	for _, remoteFile := range remoteFiles {
		name, err := destinationName(path.Base(remoteFile), options.Rename, options.RenameFunc, len(remoteFiles))
		if err != nil {
			return nil, err
		}
		destination := filepath.Join(options.LocalDir, name)
		skip, err := localDestinationPolicy(destination, overwrite, skipExisting)
		if err != nil {
			return nil, err
		}
		if skip {
			results = append(results, Result{Source: remoteFile, Destination: destination, Skipped: true})
			continue
		}
		if err := c.downloadOne(remoteFile, destination, overwrite); err != nil {
			return nil, err
		}
		if options.RemoveRemote {
			if err := c.sftp.Remove(remoteFile); err != nil {
				return nil, fmt.Errorf("remove remote file %q after download: %w", remoteFile, err)
			}
		}
		results = append(results, Result{Source: remoteFile, Destination: destination})
	}
	return results, nil
}

func (c *SFTPClient) downloadOne(remoteFile, destination string, overwrite bool) error {
	source, err := c.sftp.Open(remoteFile)
	if err != nil {
		return fmt.Errorf("open remote file %q: %w", remoteFile, err)
	}
	defer source.Close()

	temporary := destination + ".gxfer.part"
	target, err := os.OpenFile(temporary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create temporary file %q: %w", temporary, err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(temporary)
		if copyErr != nil {
			return fmt.Errorf("download %q: %w", remoteFile, copyErr)
		}
		return fmt.Errorf("close temporary file %q: %w", temporary, closeErr)
	}
	if overwrite {
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			os.Remove(temporary)
			return fmt.Errorf("replace local file %q: %w", destination, err)
		}
	}
	if err := os.Rename(temporary, destination); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("rename temporary file: %w", err)
	}
	return nil
}

// Upload transfers matching local files.
func (c *SFTPClient) Upload(options UploadOptions) ([]Result, error) {
	if err := c.connected(); err != nil {
		return nil, err
	}
	if options.RemoteDir == "" {
		options.RemoteDir = "."
	}
	if options.Overwrite && options.SkipExisting {
		return nil, fmt.Errorf("overwrite and skip-existing cannot be used together")
	}
	if options.CreateRemote {
		if err := c.Mkdir(options.RemoteDir); err != nil {
			return nil, err
		}
	}
	localFiles, err := selectLocalFiles(options.Local, options.Pattern, options.Regex)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(localFiles))
	for _, localFile := range localFiles {
		name, err := destinationName(filepath.Base(localFile), options.Rename, options.RenameFunc, len(localFiles))
		if err != nil {
			return nil, err
		}
		remoteFile := path.Join(options.RemoteDir, name)
		skip, err := c.remoteDestinationPolicy(remoteFile, options.Overwrite, options.SkipExisting)
		if err != nil {
			return nil, err
		}
		if skip {
			results = append(results, Result{Source: localFile, Destination: remoteFile, Skipped: true})
			continue
		}
		if err := c.uploadOne(localFile, remoteFile); err != nil {
			return nil, err
		}
		if options.RemoveLocal {
			if err := os.Remove(localFile); err != nil {
				return nil, fmt.Errorf("remove local file %q after upload: %w", localFile, err)
			}
		}
		results = append(results, Result{Source: localFile, Destination: remoteFile})
	}
	return results, nil
}

func (c *SFTPClient) uploadOne(localFile, remoteFile string) error {
	source, err := os.Open(localFile)
	if err != nil {
		return fmt.Errorf("open local file %q: %w", localFile, err)
	}
	defer source.Close()
	target, err := c.sftp.OpenFile(remoteFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("open remote file %q: %w", remoteFile, err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return fmt.Errorf("upload %q: %w", localFile, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close remote file %q: %w", remoteFile, closeErr)
	}
	return nil
}

// Remove deletes matching regular files from a remote directory.
func (c *SFTPClient) Remove(options RemoveOptions) ([]string, error) {
	files, err := c.List(ListOptions(options))
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(files))
	for _, file := range files {
		if file.Mode.IsDir() {
			continue
		}
		if err := c.sftp.Remove(file.Path); err != nil {
			return nil, fmt.Errorf("remove remote file %q: %w", file.Path, err)
		}
		removed = append(removed, file.Path)
	}
	return removed, nil
}

// Mkdir recursively creates a remote directory.
func (c *SFTPClient) Mkdir(remoteDir string) error {
	if err := c.connected(); err != nil {
		return err
	}
	if remoteDir == "" || remoteDir == "." || remoteDir == "/" {
		return nil
	}
	clean := path.Clean(remoteDir)
	current := ""
	if path.IsAbs(clean) {
		current = "/"
	}
	for _, part := range splitRemotePath(clean) {
		current = path.Join(current, part)
		info, err := c.sftp.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("remote path %q is not a directory", current)
			}
			continue
		}
		if err := c.sftp.Mkdir(current); err != nil {
			return fmt.Errorf("create remote directory %q: %w", current, err)
		}
		if err := c.sftp.Chmod(current, 0755); err != nil {
			return fmt.Errorf("chmod remote directory %q: %w", current, err)
		}
	}
	return nil
}

func (c *SFTPClient) connected() error {
	if c.sftp == nil {
		return fmt.Errorf("client is not connected")
	}
	return nil
}

func (c *SFTPClient) remoteFiles(remote, pattern string, useRegex bool) ([]string, error) {
	if remote == "" {
		remote = "."
	}
	info, err := c.sftp.Stat(remote)
	if err != nil {
		return nil, fmt.Errorf("stat remote path %q: %w", remote, err)
	}
	if !info.IsDir() {
		return []string{remote}, nil
	}
	files, err := c.List(ListOptions{Directory: remote, Pattern: pattern, Regex: useRegex})
	if err != nil {
		return nil, err
	}
	selected := make([]string, 0, len(files))
	for _, file := range files {
		if !file.Mode.IsDir() {
			selected = append(selected, file.Path)
		}
	}
	return selected, nil
}

func localDestinationPolicy(destination string, overwrite, skipExisting bool) (bool, error) {
	_, err := os.Stat(destination)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat local destination %q: %w", destination, err)
	}
	if overwrite {
		return false, nil
	}
	if skipExisting {
		return true, nil
	}
	return false, fmt.Errorf("local destination %q already exists", destination)
}

func (c *SFTPClient) remoteDestinationPolicy(destination string, overwrite, skipExisting bool) (bool, error) {
	_, err := c.sftp.Stat(destination)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat remote destination %q: %w", destination, err)
	}
	if overwrite {
		return false, nil
	}
	if skipExisting {
		return true, nil
	}
	return false, fmt.Errorf("remote destination %q already exists", destination)
}

func selectLocalFiles(local, pattern string, useRegex bool) ([]string, error) {
	if local == "" {
		local = "."
	}
	info, err := os.Stat(local)
	if err != nil {
		return nil, fmt.Errorf("stat local path %q: %w", local, err)
	}
	if !info.IsDir() {
		return []string{local}, nil
	}
	m, err := newMatcher(pattern, useRegex)
	if err != nil {
		return nil, err
	}
	entries, err := ioutil.ReadDir(local)
	if err != nil {
		return nil, fmt.Errorf("list local directory %q: %w", local, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !m.match(entry.Name()) {
			continue
		}
		files = append(files, filepath.Join(local, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func splitRemotePath(remote string) []string {
	remote = strings.Trim(remote, "/")
	if remote == "" {
		return nil
	}
	return strings.Split(remote, "/")
}
