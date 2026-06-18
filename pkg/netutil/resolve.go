package netutil

import (
	"fmt"
	"strings"
)

type ResolveInfo struct {
	Host string
	Port string
	IP   string
}

func ParseResolve(value string) (*ResolveInfo, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("must be host:port:ip")
	}

	return &ResolveInfo{
		Host: parts[0],
		Port: parts[1],
		IP:   parts[2],
	}, nil
}
