package main

const usageText = `
gnicstat - real-time NIC / network monitor

Usage:
  gnicstat [duration] [count] [options]
`

const helpText = `
Arguments:
  duration           seconds between updates (default: 1)
  count              number of iterations (-1 = infinite)

Options:
  -d, --duration     update interval in seconds (overrides argument)
  -c, --count        number of iterations (overrides argument)

  -g, --global       show global stats only

  -i, --interface    interface filter (e.g. eth0,eth1)
      --no-lo        exclude loopback interface (default: true)

  -h, --help         show help
  -v, --version      show version

Examples:
  gnicstat
  gnicstat 1
  gnicstat 1 10

  gnicstat -d 1 -c 10
  gnicstat -i eth0
  gnicstat -i eth0,eth1

  gnicstat --global
  gnicstat --global 1 5

Description:
  Default mode (NIC):
    Displays per-interface traffic statistics:
      rx / tx   : traffic in Mbps
      util      : link utilization (%)
      err/drop  : error and drop counters

  Global mode (--global):
    Displays system-wide protocol statistics:
      tcp / udp / icmp : packet counts
      retrans          : TCP retransmissions
      errs             : protocol errors

Notes:
  - Positional arguments (duration, count) are optional
  - Command-line options override positional arguments
  - NIC and Global modes are independent outputs
`