package xfer

import (
	"context"
	"fmt"
	"time"
)

// ClientFactory creates a disconnected transfer client.
type ClientFactory func() (Client, error)

// MonitorOptions controls polling-based downloads.
type MonitorOptions struct {
	Interval time.Duration
	Download DownloadOptions
	OnResult func(Result)
	OnError  func(error)
}

// Monitor polls for new files until ctx is cancelled. A failed connection or
// transfer is closed and recreated on the next polling cycle.
func Monitor(ctx context.Context, factory ClientFactory, options MonitorOptions) error {
	if ctx == nil {
		return fmt.Errorf("monitor context is required")
	}
	if factory == nil {
		return fmt.Errorf("monitor client factory is required")
	}
	if options.Interval <= 0 {
		return fmt.Errorf("monitor interval must be greater than zero")
	}
	options.Download.Mode = DownloadModeSkipWrite
	options.Download.SkipExisting = true
	options.Download.Overwrite = false

	var client Client
	closeClient := func() {
		if client != nil {
			_ = client.Close()
			client = nil
		}
	}
	defer closeClient()

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if client == nil {
			var err error
			client, err = factory()
			if err == nil && client == nil {
				err = fmt.Errorf("monitor client factory returned nil")
			}
			if err == nil {
				err = client.Connect()
			}
			if err != nil {
				closeClient()
				if options.OnError != nil {
					options.OnError(err)
				}
				if !waitMonitor(ctx, options.Interval) {
					return nil
				}
				continue
			}
		}

		results, err := client.Download(options.Download)
		if err != nil {
			closeClient()
			if options.OnError != nil {
				options.OnError(err)
			}
		} else if options.OnResult != nil {
			for _, result := range results {
				if !result.Skipped {
					options.OnResult(result)
				}
			}
		}
		if !waitMonitor(ctx, options.Interval) {
			return nil
		}
	}
}

func waitMonitor(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
