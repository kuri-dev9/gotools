// Package vault provides access to credentials stored in HashiCorp Vault.
package vault

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	DefaultNamespace = "prod_T-PANI"
	DefaultKVPath    = "tpanikv"
	DefaultPort      = "8200"
	DefaultTimeout   = 5 * time.Second
	DefaultHostsPath = "/etc/hosts"
)

// Endpoint identifies a Vault server. IP is used for the connection while Host
// is retained for the request URL and TLS server name.
type Endpoint struct {
	Host string
	IP   string
}

// Option configures a Client.
type Option func(*Client)

// WithNamespace sets the Vault namespace sent in X-Vault-Namespace.
func WithNamespace(namespace string) Option {
	return func(c *Client) { c.namespace = namespace }
}

// WithKVPath sets the KV mount path.
func WithKVPath(path string) Option {
	return func(c *Client) { c.kvPath = strings.Trim(path, "/") }
}

// WithTimeout sets the timeout for each endpoint request.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) { c.timeout = timeout }
}

// WithHostsPath changes the hosts file used for endpoint discovery.
func WithHostsPath(path string) Option {
	return func(c *Client) { c.hostsPath = path }
}

// WithHTTPClient supplies an HTTP client. It is mainly useful when an
// application needs a custom proxy, CA configuration, or test transport.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) { c.httpClient = client }
}

// WithInsecureTLS controls TLS certificate verification. It defaults to true
// for compatibility with the previous Vault client.
func WithInsecureTLS(insecure bool) Option {
	return func(c *Client) { c.insecureTLS = insecure }
}

// Client is a reusable Vault KV client.
type Client struct {
	namespace   string
	kvPath      string
	timeout     time.Duration
	hostsPath   string
	insecureTLS bool
	httpClient  *http.Client
	endpoints   []Endpoint
	mu          sync.Mutex
	clients     map[Endpoint]*http.Client
}

// New returns a Vault client and discovers endpoints from /etc/hosts.
func New(options ...Option) *Client {
	c := &Client{
		namespace:   DefaultNamespace,
		kvPath:      DefaultKVPath,
		timeout:     DefaultTimeout,
		hostsPath:   DefaultHostsPath,
		insecureTLS: true,
		clients:     make(map[Endpoint]*http.Client),
	}
	for _, option := range options {
		option(c)
	}
	c.loadHosts()
	return c
}

// AddResolve appends an explicit host-to-IP endpoint.
func (c *Client) AddResolve(host, ip string) {
	host = strings.TrimSpace(host)
	ip = strings.TrimSpace(ip)
	if host == "" || ip == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endpoints = append(c.endpoints, Endpoint{Host: host, IP: ip})
}

// Endpoints returns a copy of the currently configured endpoints.
func (c *Client) Endpoints() []Endpoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	endpoints := make([]Endpoint, len(c.endpoints))
	copy(endpoints, c.endpoints)
	return endpoints
}

// Get retrieves and flattens KV v1 or KV v2 data for host and user.
func (c *Client) Get(host, user string) (map[string]string, error) {
	if strings.TrimSpace(host) == "" || strings.TrimSpace(user) == "" {
		return nil, fmt.Errorf("host and user are required")
	}
	if c.namespace == "" {
		return nil, fmt.Errorf("Vault namespace is required")
	}
	if c.kvPath == "" {
		return nil, fmt.Errorf("Vault KV path is required")
	}
	if c.timeout <= 0 {
		return nil, fmt.Errorf("Vault timeout must be greater than zero")
	}
	endpoints := c.Endpoints()
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no Vault endpoints found")
	}

	path := c.kvPath + "/data/" + url.PathEscape(host) + "/" + url.PathEscape(user)
	var failures []string

	for _, endpoint := range endpoints {
		data, retry, err := c.get(endpoint, path)
		if err == nil {
			return data, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", endpoint.Host, err))
		if !retry {
			return nil, err
		}
	}

	return nil, fmt.Errorf("all Vault endpoints failed: %s", strings.Join(failures, "; "))
}

func (c *Client) get(endpoint Endpoint, path string) (map[string]string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	rawURL := "https://" + net.JoinHostPort(endpoint.Host, DefaultPort) + "/v1/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Vault-Namespace", c.namespace)

	resp, err := c.clientFor(endpoint).Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, true, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, true, httpError(resp.StatusCode, body)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, false, httpError(resp.StatusCode, body)
	}

	data, err := parseVaultData(body)
	if err != nil {
		return nil, false, err
	}
	return data, false, nil
}

func (c *Client) clientFor(endpoint Endpoint) *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if client, ok := c.clients[endpoint]; ok {
		return client
	}

	dialer := &net.Dialer{Timeout: c.timeout, KeepAlive: 30 * time.Second}
	target := net.JoinHostPort(endpoint.IP, DefaultPort)
	origin := net.JoinHostPort(endpoint.Host, DefaultPort)
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address == origin {
				address = target
			}
			return dialer.DialContext(ctx, network, address)
		},
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: c.insecureTLS,
			MinVersion:         tls.VersionTLS12,
			ServerName:         endpoint.Host,
		},
		TLSHandshakeTimeout: c.timeout,
	}
	client := &http.Client{Transport: transport, Timeout: c.timeout}
	c.clients[endpoint] = client
	return client
}

func (c *Client) loadHosts() {
	file, err := os.Open(c.hostsPath)
	if err != nil {
		return
	}
	defer file.Close()

	content, err := ioutil.ReadAll(file)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" || !strings.Contains(line, "vault") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, host := range fields[1:] {
			if strings.Contains(host, "vault") {
				c.endpoints = append(c.endpoints, Endpoint{Host: host, IP: fields[0]})
			}
		}
	}
}

func parseVaultData(body []byte) (map[string]string, error) {
	var response struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode Vault response: %w", err)
	}

	var outer map[string]json.RawMessage
	if len(response.Data) == 0 || string(response.Data) == "null" {
		return map[string]string{}, nil
	}
	if err := json.Unmarshal(response.Data, &outer); err != nil {
		return nil, fmt.Errorf("decode Vault data: %w", err)
	}
	if nested, ok := outer["data"]; ok {
		var inner map[string]json.RawMessage
		if json.Unmarshal(nested, &inner) == nil {
			outer = inner
		}
	}

	result := make(map[string]string, len(outer))
	for key, value := range outer {
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			result[key] = text
			continue
		}
		var scalar interface{}
		if err := json.Unmarshal(value, &scalar); err != nil {
			return nil, fmt.Errorf("decode Vault key %q: %w", key, err)
		}
		switch scalar.(type) {
		case nil, bool, float64:
			result[key] = fmt.Sprint(scalar)
		default:
			encoded, _ := json.Marshal(scalar)
			result[key] = string(encoded)
		}
	}
	return result, nil
}

func httpError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if message == "" {
		return fmt.Errorf("Vault returned HTTP %d", status)
	}
	return fmt.Errorf("Vault returned HTTP %d: %s", status, message)
}
