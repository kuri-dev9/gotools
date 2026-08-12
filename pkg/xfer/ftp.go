package xfer

import (
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/jlaffaye/ftp"
)

// FTPClient implements Client using plain FTP.
type FTPClient struct {
	config Config
	conn   net.Conn
	ftp    *ftp.ServerConn
}

// deadlineConn applies an idle timeout to every socket operation. DialTimeout
// only limits connection establishment; without this wrapper an established
// FTP control connection can block forever waiting for a server response.
type deadlineConn struct {
	net.Conn
	timeout time.Duration
}

func (c *deadlineConn) Read(buffer []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(buffer)
}

func (c *deadlineConn) Write(buffer []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(buffer)
}

// NewFTP creates a disconnected FTP client.
func NewFTP(config Config) (*FTPClient, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.FTPMode == "" {
		config.FTPMode = FTPModePassive
	}
	if config.FTPMode != FTPModePassive && config.FTPMode != FTPModeActive {
		return nil, fmt.Errorf("unsupported FTP mode %q", config.FTPMode)
	}
	if config.Port == 0 {
		config.Port = DefaultFTPPort
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	return &FTPClient{config: config}, nil
}

// Connect establishes and authenticates an FTP session.
func (c *FTPClient) Connect() error {
	if c.ftp != nil {
		return nil
	}
	address := net.JoinHostPort(c.config.Host, fmt.Sprint(c.config.Port))
	rawConnection, err := net.DialTimeout("tcp", address, c.config.Timeout)
	if err != nil {
		return fmt.Errorf("connect FTP: %w", err)
	}
	connection := &deadlineConn{Conn: rawConnection, timeout: c.config.Timeout}
	server, err := ftp.Dial(
		address,
		ftp.DialWithNetConn(connection),
		ftp.DialWithTimeout(c.config.Timeout),
		ftp.DialWithActiveMode(c.config.FTPMode == FTPModeActive),
	)
	if err != nil {
		connection.Close()
		return fmt.Errorf("start FTP session: %w", err)
	}
	if err := server.Login(c.config.Username, c.config.Password); err != nil {
		server.Quit()
		return fmt.Errorf("FTP login failed: %w", err)
	}
	c.conn = connection
	c.ftp = server
	return nil
}

// Close terminates the FTP session.
func (c *FTPClient) Close() error {
	if c.ftp == nil {
		return nil
	}
	err := c.ftp.Quit()
	c.ftp = nil
	c.conn = nil
	return err
}

// Check verifies login and remote directory listing.
func (c *FTPClient) Check(remoteDir string) error {
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
	if err := c.ftp.NoOp(); err != nil {
		return fmt.Errorf("FTP connection check failed: %w", err)
	}
	isDirectory, err := c.directoryExists(remoteDir)
	if err != nil {
		return err
	}
	if !isDirectory {
		return fmt.Errorf("remote path %q is not a directory", remoteDir)
	}
	if _, err := c.listEntries(remoteDir); err != nil {
		return err
	}
	return nil
}

// List returns matching entries in an FTP directory.
func (c *FTPClient) List(options ListOptions) ([]File, error) {
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
	entries, err := c.listEntries(options.Directory)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		if !m.match(entry.Name) {
			continue
		}
		mode := os.FileMode(0)
		if entry.Type == ftp.EntryTypeFolder {
			mode = os.ModeDir
		}
		files = append(files, File{
			Name: entry.Name, Path: path.Join(options.Directory, entry.Name),
			Size: int64(entry.Size), Mode: mode, ModTime: entry.Time,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// Download transfers matching FTP files through local temporary files.
func (c *FTPClient) Download(options DownloadOptions) ([]Result, error) {
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
			if err := c.ftp.Delete(remoteFile); err != nil {
				return nil, fmt.Errorf("remove remote file %q after download: %w", remoteFile, err)
			}
		}
		results = append(results, Result{Source: remoteFile, Destination: destination})
	}
	return results, nil
}

func (c *FTPClient) downloadOne(remoteFile, destination string, overwrite bool) error {
	response, err := c.ftp.Retr(remoteFile)
	if err != nil {
		return fmt.Errorf("open remote file %q: %w", remoteFile, err)
	}

	temporary := destination + ".gxfer.part"
	target, err := os.OpenFile(temporary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		response.Close()
		return fmt.Errorf("create temporary file %q: %w", temporary, err)
	}
	_, copyErr := io.Copy(target, response)
	targetCloseErr := target.Close()
	responseCloseErr := response.Close()
	if copyErr != nil || targetCloseErr != nil || responseCloseErr != nil {
		os.Remove(temporary)
		if copyErr != nil {
			return fmt.Errorf("download %q: %w", remoteFile, copyErr)
		}
		if targetCloseErr != nil {
			return fmt.Errorf("close temporary file %q: %w", temporary, targetCloseErr)
		}
		return fmt.Errorf("close remote file %q: %w", remoteFile, responseCloseErr)
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

// Upload transfers matching local files to FTP.
func (c *FTPClient) Upload(options UploadOptions) ([]Result, error) {
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
		exists, err := c.fileExists(remoteFile)
		if err != nil {
			return nil, err
		}
		if exists && !options.Overwrite {
			if options.SkipExisting {
				results = append(results, Result{Source: localFile, Destination: remoteFile, Skipped: true})
				continue
			}
			return nil, fmt.Errorf("remote destination %q already exists", remoteFile)
		}
		source, err := os.Open(localFile)
		if err != nil {
			return nil, fmt.Errorf("open local file %q: %w", localFile, err)
		}
		uploadErr := c.ftp.Stor(remoteFile, source)
		closeErr := source.Close()
		if uploadErr != nil {
			return nil, fmt.Errorf("upload %q: %w", localFile, uploadErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close local file %q: %w", localFile, closeErr)
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

// Remove deletes matching regular files from an FTP directory.
func (c *FTPClient) Remove(options RemoveOptions) ([]string, error) {
	files, err := c.List(ListOptions(options))
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(files))
	for _, file := range files {
		if file.Mode.IsDir() {
			continue
		}
		if err := c.ftp.Delete(file.Path); err != nil {
			return nil, fmt.Errorf("remove remote file %q: %w", file.Path, err)
		}
		removed = append(removed, file.Path)
	}
	return removed, nil
}

// Mkdir recursively creates an FTP directory.
func (c *FTPClient) Mkdir(remoteDir string) error {
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
		exists, err := c.directoryExists(current)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := c.ftp.MakeDir(current); err != nil {
			return fmt.Errorf("create remote directory %q: %w", current, err)
		}
	}
	return nil
}

func (c *FTPClient) connected() error {
	if c.ftp == nil {
		return fmt.Errorf("client is not connected")
	}
	return nil
}

func (c *FTPClient) remoteFiles(remote, pattern string, useRegex bool) ([]string, error) {
	if remote == "" {
		remote = "."
	}
	isDirectory, err := c.directoryExists(remote)
	if err != nil {
		return nil, err
	}
	if !isDirectory {
		if _, err := c.ftp.FileSize(remote); err != nil {
			return nil, fmt.Errorf("stat remote path %q: %w", remote, err)
		}
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

func (c *FTPClient) fileExists(remoteFile string) (bool, error) {
	directory, name := path.Split(remoteFile)
	if directory == "" {
		directory = "."
	}
	entries, err := c.listEntries(path.Clean(directory))
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name == name && entry.Type != ftp.EntryTypeFolder {
			return true, nil
		}
	}
	return false, nil
}

func (c *FTPClient) directoryExists(remoteDir string) (bool, error) {
	current, err := c.ftp.CurrentDir()
	if err != nil {
		return false, fmt.Errorf("get current FTP directory: %w", err)
	}
	if err := c.ftp.ChangeDir(remoteDir); err != nil {
		return false, nil
	}
	if err := c.ftp.ChangeDir(current); err != nil {
		return false, fmt.Errorf("restore FTP directory %q: %w", current, err)
	}
	return true, nil
}

func (c *FTPClient) listEntries(remoteDir string) ([]*ftp.Entry, error) {
	entries, err := c.ftp.List(remoteDir)
	if err != nil {
		return nil, fmt.Errorf(
			"list %q via FTP data connection (control %s:%d): %w",
			remoteDir, c.config.Host, c.config.Port, err,
		)
	}
	return entries, nil
}
