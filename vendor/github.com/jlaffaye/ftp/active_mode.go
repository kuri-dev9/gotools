package ftp

// DialWithActiveMode configures PORT/EPRT active data connections.
// Passive EPSV/PASV data connections are used when active is false.
func DialWithActiveMode(active bool) DialOption {
	return DialOption{func(options *dialOptions) {
		options.activeMode = active
	}}
}
