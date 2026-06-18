package main

const usageText = `
gnode - system hardware / usage summary tool

Usage:
  gnode [options]
`

var helpText = `
Options:
      --json         output in JSON format

      --type         filter output
                     cpu, mem, os, disk, net, model, usage

  -u, --usage        show usage only (CPU / Load / MEM)

  -h, --help         show help
  -v, --version      show version

Examples:
  gnode
  gnode --json

  gnode --type cpu
  gnode --type cpu,mem
  gnode --type usage

  gnode -u
  gnode -u --json

Description:
  Default mode:
    Displays system hardware information:
      cpu        : CPU model and core count
      mem        : memory size (total / OS / reserved)
      os/kernel  : operating system and kernel version
      disk       : disk list and size
      net        : network interfaces (IP / link / speed)
      model      : server model information

  Usage mode (--type usage or -u):
    Displays system usage statistics:
      CPU        : CPU utilization (%)
      Load       : system load average (1m / 5m / 15m)
      MEM        : memory usage (% and used/total)

Notes:
  - --type allows selective output of specific categories
  - -u (--usage) is a shortcut for usage-only mode
  - JSON output can be combined with all options
`