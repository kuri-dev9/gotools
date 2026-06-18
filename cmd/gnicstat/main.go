package main

import (
	"fmt"
	"runtime"
	"strconv"

	"gtools/pkg/cli"
	"gtools/pkg/nicstat"
	"gtools/pkg/version"

	"github.com/spf13/pflag"
)

const version_info = "1.0.0"

var (
	duration        = pflag.IntP("duration", "d", 1, "print duration seconds")
	count           = pflag.IntP("count", "c", -1, "number of iterations")
	globalOnly      = pflag.BoolP("global", "g", false, "show global stats only")
	interfaceFilter = pflag.StringP("interface", "i", "", "interface filter (eth0,eth1)")
	noLoopback      = pflag.Bool("no-lo", true, "exclude loopback")
	helpFlag        = pflag.BoolP("help", "h", false, "help")
	versionFlag     = pflag.BoolP("version", "v", false, "version")
)

func init() {
    runtime.GOMAXPROCS(1)
}

func main() {
	pflag.Usage = func() {
		cli.PrintCustomHelp(usageText, helpText)
	}

	pflag.Parse()

	if *helpFlag {
		cli.PrintCustomHelp(usageText, helpText)
		return
	}

	if *versionFlag {
		version.Version = version_info
		version.Print()
		return
	}

	args := pflag.Args()
	if len(args) >= 1 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			*duration = v
		}
	}
	if len(args) >= 2 {
		if v, err := strconv.Atoi(args[1]); err == nil {
			*count = v
		}
	}

	cfg := nicstat.Config{
		Duration:        *duration,
		Count:           *count,
		GlobalDuration:  *duration,
		GlobalOnly:      *globalOnly,
		InterfaceFilter: *interfaceFilter,
		ExcludeLo:       *noLoopback,
	}

	err := nicstat.Run(cfg)
	if err != nil {
		fmt.Println("error:", err)
	}
}
