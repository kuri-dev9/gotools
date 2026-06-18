package netutil

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

func BuildTransport(tlsConfig *tls.Config, resolve *ResolveInfo) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialer.DialContext,
		TLSClientConfig:     tlsConfig,
		TLSHandshakeTimeout: 30 * time.Second,
	}

	if resolve != nil {
		target := net.JoinHostPort(resolve.IP, resolve.Port)
		origin := net.JoinHostPort(resolve.Host, resolve.Port)

		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == origin {
				return dialer.DialContext(ctx, network, target)
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}

	return tr
}
