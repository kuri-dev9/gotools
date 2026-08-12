package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"gtools/pkg/cli"
	"gtools/pkg/xfer"

	"github.com/spf13/pflag"
)

func runConfigCommand(forceMonitor bool, args []string) error {
	args = normalizeLegacyArgs(args)
	name := "run"
	if forceMonitor {
		name = "monitor"
	}
	flags := pflag.NewFlagSet("gxfer "+name, pflag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configFile := flags.StringP("config", "c", "", "ACTION INI config")
	help := flags.BoolP("help", "h", false, "show help")
	flags.Usage = func() { cli.PrintCustomHelp(commandHelp(name)) }
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *help {
		flags.Usage()
		return nil
	}
	if *configFile == "" {
		return fmt.Errorf("--config is required")
	}
	if len(flags.Args()) != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	plan, err := xfer.LoadJobPlan(*configFile, time.Now())
	if err != nil {
		return err
	}
	if forceMonitor {
		return executeMonitorPlan(plan.Read)
	}
	return executeJobPlan(plan)
}

func executeJobPlan(plan xfer.JobPlan) error {
	for _, jobs := range [][]xfer.Job{plan.Read, plan.Write, plan.Remove} {
		for _, job := range jobs {
			if err := executeJob(job); err != nil {
				return fmt.Errorf("job %q failed: %w", job.Name, err)
			}
		}
	}
	return nil
}

func executeJob(job xfer.Job) error {
	commandContext, err := prepareJobContext(job.Settings, true)
	if err != nil {
		return err
	}
	defer commandContext.close()

	switch job.Mode {
	case xfer.JobModeGet:
		return executeJobGet(commandContext.client, job.Settings)
	case xfer.JobModePut:
		return executeJobPut(commandContext.client, job.Settings)
	case xfer.JobModeDelete:
		return executeJobDelete(commandContext.client, job.Settings)
	default:
		return fmt.Errorf("unsupported job mode %q", job.Mode)
	}
}

func prepareJobContext(settings xfer.LegacySettings, connect bool) (*commandContext, error) {
	flags := pflag.NewFlagSet("gxfer job", pflag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	common := registerCommonFlags(flags)
	return prepareContext(flags, common, settings, connect)
}

func executeJobGet(client xfer.Client, settings xfer.LegacySettings) error {
	rename, err := xfer.LegacyRename(settings.SourceTemplate, settings.DestinationPattern)
	if err != nil {
		return err
	}
	pattern, regex := jobPattern(settings)
	results, err := client.Download(xfer.DownloadOptions{
		Remote: settings.SourceDir, LocalDir: settings.DestinationDir,
		Pattern: pattern, Regex: regex, RenameFunc: rename,
		Mode: settings.DownloadMode, OnceFile: settings.CurrentSourceFile,
		RemoveRemote: settings.RemoveSource, CreateLocalDir: true,
	})
	return printResults(results, err)
}

func executeJobPut(client xfer.Client, settings xfer.LegacySettings) error {
	rename, err := xfer.LegacyRename(settings.SourceTemplate, settings.DestinationPattern)
	if err != nil {
		return err
	}
	results, err := client.Upload(xfer.UploadOptions{
		Local: settings.SourceDir, RemoteDir: settings.DestinationDir,
		Pattern: settings.SourcePattern, Regex: settings.Regex, RenameFunc: rename,
		SkipExisting: true, RemoveLocal: settings.RemoveSource, CreateRemote: true,
	})
	return printResults(results, err)
}

func executeJobDelete(client xfer.Client, settings xfer.LegacySettings) error {
	removed, err := client.Remove(xfer.RemoveOptions{
		Directory: settings.SourceDir, Pattern: settings.SourcePattern, Regex: settings.Regex,
	})
	if err != nil {
		return err
	}
	for _, name := range removed {
		fmt.Println(name)
	}
	return nil
}

type monitorRuntime struct {
	job     xfer.Job
	context *commandContext
}

func executeMonitorPlan(jobs []xfer.Job) error {
	if len(jobs) == 0 {
		return fmt.Errorf("ACTION.read requires at least one monitor job")
	}
	runtimes := make([]monitorRuntime, 0, len(jobs))
	for _, job := range jobs {
		commandContext, err := prepareJobContext(job.Settings, false)
		if err != nil {
			closeMonitorRuntimes(runtimes)
			return fmt.Errorf("prepare monitor job %q: %w", job.Name, err)
		}
		runtimes = append(runtimes, monitorRuntime{job: job, context: commandContext})
	}

	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	errors := make(chan error, len(runtimes))
	var wait sync.WaitGroup
	for _, runtime := range runtimes {
		wait.Add(1)
		go func(runtime monitorRuntime) {
			defer wait.Done()
			if err := monitorJob(ctx, runtime); err != nil {
				errors <- fmt.Errorf("monitor job %q: %w", runtime.job.Name, err)
			}
		}(runtime)
	}

	var result error
	select {
	case <-signals:
	case result = <-errors:
	}
	cancel()
	wait.Wait()
	closeMonitorRuntimes(runtimes)
	return result
}

func monitorJob(ctx context.Context, runtime monitorRuntime) error {
	rename, err := xfer.LegacyRename(
		runtime.job.Settings.SourceTemplate,
		runtime.job.Settings.DestinationPattern,
	)
	if err != nil {
		return err
	}
	initial := runtime.context.client
	runtime.context.client = nil
	usedInitial := false
	factory := func() (xfer.Client, error) {
		if !usedInitial {
			usedInitial = true
			return initial, nil
		}
		return xfer.New(runtime.context.config)
	}
	pattern, regex := jobPattern(runtime.job.Settings)
	return xfer.Monitor(ctx, factory, xfer.MonitorOptions{
		Interval: time.Duration(runtime.job.Settings.FetchInterval) * time.Second,
		Download: xfer.DownloadOptions{
			Remote:   runtime.job.Settings.SourceDir,
			LocalDir: runtime.job.Settings.DestinationDir,
			Pattern:  pattern, Regex: regex, RenameFunc: rename,
			Mode:           xfer.DownloadModeSkipWrite,
			RemoveRemote:   runtime.job.Settings.RemoveSource,
			CreateLocalDir: true,
		},
		OnResult: func(result xfer.Result) {
			fmt.Println(result.Destination)
		},
		OnError: func(err error) {
			fmt.Fprintf(os.Stderr, "error: monitor job %q: %s\n", runtime.job.Name, err)
		},
	})
}

func closeMonitorRuntimes(runtimes []monitorRuntime) {
	for _, runtime := range runtimes {
		runtime.context.close()
	}
}

func jobPattern(settings xfer.LegacySettings) (string, bool) {
	if settings.DownloadMode == xfer.DownloadModeOnce {
		return settings.CurrentSourceFile, false
	}
	return settings.SourcePattern, settings.Regex
}
