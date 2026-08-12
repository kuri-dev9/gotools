package main

import (
	"fmt"
	"os"
	"runtime"

	"gtools/pkg/cli"
	"gtools/pkg/filewatch"
	"gtools/pkg/tui"
	"gtools/pkg/version"

	"github.com/spf13/pflag"
)

const version_info = "1.0.0"

var (
	warnFlag    = pflag.IntP("warn", "w", 1, "warn minutes")
	countFlag   = pflag.IntP("count", "c", 2, "recent file count")
	ignoreFlag  = pflag.StringArrayP("ignore", "i", nil, "ignore glob")
	allFlag     = pflag.BoolP("all", "a", false, "show file time and size")
	helpFlag    = pflag.BoolP("help", "h", false, "show help")
	versionFlag = pflag.BoolP("version", "v", false, "show version")
)

func init() {
	runtime.GOMAXPROCS(1)
}

func main() {
	pflag.Usage = func() {
		cli.PrintCustomHelp(helpText)
	}
	pflag.Parse()

	if *helpFlag {
		cli.PrintCustomHelp(helpText)
		return
	}
	if *versionFlag {
		version.Version = "gwatch v" + version_info
		fmt.Println(version.Version)
		return
	}

	target := ""
	if pflag.NArg() > 0 {
		target = pflag.Arg(0)
	}

	report, err := filewatch.Run(filewatch.Options{
		ConfPath: target,
		Count:    *countFlag,
		Warn:     *warnFlag,
		All:      *allFlag,
		Ignore:   *ignoreFlag,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	printReport(report, *allFlag)
}

func printReport(report *filewatch.Report, all bool) {
	printed := 0
	for _, cfg := range report.Configs {
		if cfg.Error == "" && len(cfg.Entries) == 0 {
			continue
		}
		if printed > 0 {
			fmt.Println()
		}
		printed++
		fmt.Println(cfg.Path)
		// fmt.Println()

		if cfg.Error != "" {
			fmt.Println(colorProblem("└─ ERROR: " + cfg.Error))
			continue
		}

		for entryIndex, entry := range cfg.Entries {
			lastEntry := entryIndex == len(cfg.Entries)-1
			fmt.Printf("%s %s : %s\n", branch(lastEntry), entry.Label, entry.Dir)
			filePrefix := branchPrefix(lastEntry)

			if entry.Error != "" {
				fmt.Println(filePrefix + colorProblem("└─ "+entry.Error))
				continue
			}
			for fileIndex, f := range entry.Files {
				lastFile := fileIndex == len(entry.Files)-1
				line := leaf(lastFile) + f.Name
				if all {
					line += fmt.Sprintf(" (%s, %s)", f.Time.Format("2006-01-02 15:04:05"), filewatch.FormatSize(f.Size))
				}
				if f.Warn {
					line = tui.ColorizeString(line, tui.Yellow)
				}
				fmt.Println(filePrefix + line)
			}
		}
	}
}

func branch(last bool) string {
	if last {
		return "└─"
	}
	return "├─"
}

func leaf(last bool) string {
	if last {
		return "└─ "
	}
	return "├─ "
}

func branchPrefix(last bool) string {
	if last {
		return "   "
	}
	return "├  "
}

func colorProblem(s string) string {
	return tui.ColorizeString(s, tui.BrightRed)
}
