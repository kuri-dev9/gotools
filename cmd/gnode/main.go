package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"gtools/pkg/cli"
	"gtools/pkg/hwinfo"
	"gtools/pkg/version"
	"gtools/pkg/tui"

	"github.com/spf13/pflag"
)

const version_info = "1.1.0"

var (
	jsonOut     = pflag.Bool("json", false, "json output")
	typeFilter  = pflag.String("type", "all", "filter")
	helpFlag    = pflag.BoolP("help", "h", false, "help")
	versionFlag = pflag.BoolP("version", "v", false, "version")
	usageOnly   = pflag.BoolP("usage", "u", false, "usage only")
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

	filters := parseFilter(*typeFilter)
	info := hwinfo.Collect(filters)

	if *usageOnly {
		filters = map[string]bool{"usage": true}
		filters["cpu"] = true
		info := hwinfo.Collect(filters)

		if *jsonOut {
			out, _ := json.MarshalIndent(info, "", "  ")
			fmt.Println(string(out))
			return
		}

		printUsageOnly(info)
	} else {
		if *jsonOut {
			out, _ := json.MarshalIndent(info, "", "  ")
			fmt.Println(string(out))
			return
		}
	
		printText(info, filters)
	}
}

func parseFilter(s string) map[string]bool {
	m := make(map[string]bool)
	if s == "all" {
		m["all"] = true
		return m
	}
	for _, v := range strings.Split(s, ",") {
		m[strings.TrimSpace(v)] = true
	}
	return m
}

func printUsageOnly(i *hwinfo.HWInfo) {
	u := i.Usage
	if u == nil {
		return
	}

	cores := float64(i.CPU.Cores)
	loadPct1 := (u.Load1 / cores) * 100
	loadPct5 := (u.Load5 / cores) * 100
	loadPct15 := (u.Load15 / cores) * 100

	fmt.Println(tui.ColorizeString("Usage", tui.Cyan) + ":")

	fmt.Printf("  %s: %.1f%%\n", "CPU", u.CPUUsage)
	fmt.Printf("    %s\n", "Load Average")
	fmt.Printf("      %-3s: %.2f (%.0f%%)\n", "1m", u.Load1, loadPct1)
	fmt.Printf("      %-3s: %.2f (%.0f%%)\n", "5m", u.Load5, loadPct5)
	fmt.Printf("      %-3s: %.2f (%.0f%%)\n", "15m", u.Load15, loadPct15)
	fmt.Printf("  %s: %.1f%% (%.1fGB / %.1fGB)\n", "MEM", u.MemUsage, u.MemUsedGB, u.MemTotalGB)
}

func printText(i *hwinfo.HWInfo, f map[string]bool) {
	if f["all"] || f["cpu"] {
		fmt.Println(tui.ColorizeString("CPU", tui.Cyan) + ":", i.CPU.Model, "(", i.CPU.Cores, "cores )")
	}
	if f["all"] || f["mem"] {
		fmt.Printf("%s: %.2fGB (OS %.1fGB, Rsv %.1fGB)\n", tui.ColorizeString("Memory", tui.Cyan), i.Memory.HWGB, i.Memory.TotalGB, i.Memory.RsvGB)
	}
	if f["all"] || f["os"] {
		fmt.Println(tui.ColorizeString("OS", tui.Cyan) + ":", i.OS.Name)
		fmt.Println(tui.ColorizeString("Kernel", tui.Cyan) + ":", i.Kernel)
	}
	if f["all"] || f["model"] {
		fmt.Println(tui.ColorizeString("Model", tui.Cyan) + ":", i.Model)
	}
	if f["all"] || f["disk"] {
		totalDiskSize := hwinfo.SumDiskSize(i.Disks)
		fmt.Printf("%s: %dEA %0.0fGB\n", tui.ColorizeString("Disks", tui.Cyan), i.DiskCount, totalDiskSize)
		mountSize := 0
		for _, d := range i.Disks {
			if mountSize < len(d.Mount) {
				mountSize = len(d.Mount)
			}
		}
		for _, d := range i.Disks {
			fmt.Printf("  %s (%s) %-*s %.2f GB\n", d.Model, d.Name, mountSize, d.Mount, d.SizeGB)
		}
	}
	if f["all"] || f["net"] {
		fmt.Printf("%s: %dEA\n", tui.ColorizeString("NICs", tui.Cyan), len(i.NICs))
		for _, n := range i.NICs {
			if n.Speed != "" {
				fmt.Printf("  %-6s (%-15s) %-4s %s\n", n.Name, n.IP, n.Link, n.Speed)
			} else {
				fmt.Printf("  %-6s (%-15s) %-4s\n", n.Name, n.IP, n.Link)
			}
		}
	}
	if f["all"] || f["pci"] {
		if len(i.CaptureCards) > 0 {
			fmt.Printf("Capture Cards: %d\n", len(i.CaptureCards))
			for _, c := range i.CaptureCards {
				fmt.Println(" ", c.Raw)
			}
		}
	}
}
