package nicstat

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"gtools/pkg/tui"
)

var ifaceSpeed = make(map[string]float64)

type Config struct {
	Duration        int
	Count           int
	GlobalDuration  int
	GlobalOnly      bool
	InterfaceFilter string
	ExcludeLo       bool
}

type NicStat struct {
	RxBytes uint64
	TxBytes uint64
	Err     uint64
	Drop    uint64
}

type TcpStat struct {
	Retrans uint64
}

type ProtoStat struct {
	Tcp  uint64
	Udp  uint64
	Icmp uint64
}

type GlobalRow struct {
	Time     string
	TCP      uint64
	UDP      uint64
	ICMP     uint64
	Retrans  uint64
}

func Run(cfg Config) error {
	if cfg.GlobalOnly {
		prevTcp := readTcpStats()
		prevProto := readProtoStats()
		globalLLineCount := 0

		for {
			time.Sleep(time.Duration(cfg.Duration) * time.Second)
			currTcp := readTcpStats()
			currProto := readProtoStats()
			printGlobalTable(prevTcp, currTcp, prevProto, currProto, cfg.Duration, cfg.GlobalOnly, &globalLLineCount)

			prevTcp = currTcp
			prevProto = currProto
			if cfg.Count > 0 {
				cfg.Count--
				if cfg.Count == 0 {
					break
				}
			}
		}
		return nil
	}

	if err := validateInterfaces(cfg.InterfaceFilter); err != nil {
		return err
	}

	prevNic := readNicStats(cfg)
	// prevTcp := readTcpStats()
	// prevProto := readProtoStats()

	initInterfaceSpeeds(prevNic)

	// tick := 0
	lineCount := 0

	time.Sleep(2 * time.Second)
	first := readNicStats(cfg)

	printNicTable(prevNic, first, cfg, &lineCount)

	prevNic = first

	for {
		time.Sleep(time.Duration(cfg.Duration) * time.Second)

		currNic := readNicStats(cfg)
		// currTcp := readTcpStats()
		// currProto := readProtoStats()

		printNicTable(prevNic, currNic, cfg, &lineCount)

		// tick += cfg.Duration

		// if tick >= cfg.GlobalDuration {
		// 	printGlobalTable(prevTcp, currTcp, prevProto, currProto, cfg.GlobalDuration, cfg.GlobalOnly, &lineCount)
		// 	prevTcp = currTcp
		// 	prevProto = currProto
		// 	tick = 0
		// }

		prevNic = currNic

		if cfg.Count > 0 {
			cfg.Count--
			if cfg.Count == 0 {
				break
			}
		}
	}

	return nil
}

func initInterfaceSpeeds(nics map[string]NicStat) {
    for name := range nics {
        ifaceSpeed[name] = getInterfaceSpeed(name)
    }
}

func printNicTable(prevNic, currNic map[string]NicStat, cfg Config, lineCount *int) {
	now := time.Now().Format("15:04:05")
	table := tui.NewTable([]string{
		"Time", "IF", "RX(bps)", "TX(bps)", "Util", "Err", "Drop",
	})

	// width (동일 유지)
	table.SetWidth(0, 8)
	table.SetWidth(1, 8)
	table.SetWidth(2, 8)
	table.SetWidth(3, 8)
	table.SetWidth(4, 8)
	table.SetWidth(5, 8)
	table.SetWidth(6, 8)

	for i := 0; i < 7; i++ {
		table.SetHeaderAlign(i, tui.AlignCenter)
	}

	table.SetAlign(0, tui.AlignCenter)
	table.SetAlign(1, tui.AlignLeft)
	table.SetAlign(2, tui.AlignRight)
	table.SetAlign(3, tui.AlignRight)
	table.SetAlign(4, tui.AlignRight)
	table.SetAlign(5, tui.AlignRight)
	table.SetAlign(6, tui.AlignRight)

	// table.SetIndent(2, 1)
	// table.SetIndent(3, 1)
	// table.SetIndent(4, 1)
	table.SetIndent(5, 1)
	table.SetIndent(6, 1)

	table.SeparatorColor = tui.BrightBlue
	table.HeaderColor = tui.BrightBlue + tui.Underline + tui.Bold
	table.SetSeparator(1, true)
	table.SetSeparator(4, true)

	table.EnsureWidths()
	height := getTerminalHeight()
	visibleLine := *lineCount % (height - 5)
	if visibleLine == 0 {
		table.PrintHeader()
	}

	keys := make([]string, 0, len(currNic))
	for k := range currNic {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		prev := prevNic[name]
		curr := currNic[name]

		rxMbps := float64(curr.RxBytes-prev.RxBytes) * 8 / float64(cfg.Duration) / 1000000
		txMbps := float64(curr.TxBytes-prev.TxBytes) * 8 / float64(cfg.Duration) / 1000000
		util := getUtil(rxMbps + txMbps, name)

		table.PrintRow([]string{
			tui.ColorizeString(now, tui.White),
			tui.ColorizeString(name, tui.White),
			tui.ColorizeTraffic(rxMbps) + tui.ColorizeString("M", tui.BrightBlack),
			tui.ColorizeTraffic(txMbps) + tui.ColorizeString("M", tui.BrightBlack),
			tui.ColorizeNumber(uint64(util)) + tui.ColorizeString("%", tui.BrightBlack),
			tui.ColorizeError(curr.Err-prev.Err),
			tui.ColorizeError(curr.Drop-prev.Drop),
		})
		*lineCount++
	}
}

func printGlobalTable(prevTcp, currTcp TcpStat, prevProto, currProto ProtoStat, sec int, globalOnly bool, globalLineCount *int) {
	now := time.Now().Format("15:04:05")
	table := tui.NewTable([]string{
		"Time", "IF", "TCP", "UDP", "ICMP", "Retrans",
	})

	// NIC과 동일한 스타일 유지 (핵심)
	table.SetWidth(0, 8)
	table.SetWidth(1, 8)
	table.SetWidth(2, 8)
	table.SetWidth(3, 8)
	table.SetWidth(4, 8)
	table.SetWidth(5, 8)

	for i := 0; i < 6; i++ {
		table.SetHeaderAlign(i, tui.AlignCenter)
	}

	table.SetAlign(0, tui.AlignCenter)
	table.SetAlign(1, tui.AlignLeft)
	table.SetAlign(2, tui.AlignRight)
	table.SetAlign(3, tui.AlignRight)
	table.SetAlign(4, tui.AlignRight)
	table.SetAlign(5, tui.AlignRight)

	table.SetIndent(2, 1)
	table.SetIndent(3, 1)
	table.SetIndent(4, 1)
	table.SetIndent(5, 1)

	table.SeparatorColor = tui.BrightBlue
	table.HeaderColor = tui.BrightBlue + tui.Underline + tui.Bold
	table.SetSeparator(1, true)
	table.SetSeparator(4, true)

	table.EnsureWidths()
	if globalOnly && globalLineCount != nil {
		height := getTerminalHeight()
		table.PrintHeaderIfNeeded(*globalLineCount, height - 3)
		*globalLineCount++
	} else {
		table.PrintHeader()
	}

	tcp := currProto.Tcp - prevProto.Tcp
	udp := currProto.Udp - prevProto.Udp
	icmp := currProto.Icmp - prevProto.Icmp
	rtx := float64(currTcp.Retrans-prevTcp.Retrans) / float64(sec)

	table.PrintRow([]string{
		tui.ColorizeString(now, tui.White),
		tui.ColorizeString("Iface", tui.White),
		tui.ColorizeString(fmt.Sprintf("%d", tcp), tui.Yellow),
		tui.ColorizeString(fmt.Sprintf("%d", udp), tui.Yellow),
		tui.ColorizeString(fmt.Sprintf("%d", icmp), tui.Yellow),
		tui.ColorizeError(uint64(rtx)),
	})
}

func readNicStats(cfg Config) map[string]NicStat {
	file, _ := os.Open("/proc/net/dev")
	defer file.Close()

	stats := make(map[string]NicStat)
	scanner := bufio.NewScanner(file)

	filter := parseFilter(cfg.InterfaceFilter)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, ":") {
			continue
		}

		parts := strings.Split(line, ":")
		iface := strings.TrimSpace(parts[0])

		if cfg.ExcludeLo && iface == "lo" {
			continue
		}

		if len(filter) > 0 && !filter[iface] {
			continue
		}

		if !isValidInterface(iface) {
			continue
		}

		fields := strings.Fields(parts[1])

		rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
		rxErr, _ := strconv.ParseUint(fields[2], 10, 64)
		rxDrop, _ := strconv.ParseUint(fields[3], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[8], 10, 64)

		stats[iface] = NicStat{
			RxBytes: rxBytes,
			TxBytes: txBytes,
			Err:     rxErr,
			Drop:    rxDrop,
		}
	}

	return stats
}

func readTcpStats() TcpStat {
	file, _ := os.Open("/proc/net/snmp")
	defer file.Close()

	scanner := bufio.NewScanner(file)

	var keys []string
	var values []string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Tcp:") {
			if keys == nil {
				keys = strings.Fields(line)[1:]
			} else {
				values = strings.Fields(line)[1:]
				break
			}
		}
	}

	stat := TcpStat{}

	for i, k := range keys {
		if k == "RetransSegs" {
			v, _ := strconv.ParseUint(values[i], 10, 64)
			stat.Retrans = v
		}
	}

	return stat
}

func readProtoStats() ProtoStat {
	file, _ := os.Open("/proc/net/snmp")
	defer file.Close()

	scanner := bufio.NewScanner(file)

	stat := ProtoStat{}

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "Tcp:") {
			scanner.Scan()
			fields := strings.Fields(scanner.Text())
			v, _ := strconv.ParseUint(fields[10], 10, 64)
			stat.Tcp = v
		}

		if strings.HasPrefix(line, "Udp:") {
			scanner.Scan()
			fields := strings.Fields(scanner.Text())
			v, _ := strconv.ParseUint(fields[0], 10, 64)
			stat.Udp = v
		}

		if strings.HasPrefix(line, "Icmp:") {
			scanner.Scan()
			fields := strings.Fields(scanner.Text())
			v, _ := strconv.ParseUint(fields[0], 10, 64)
			stat.Icmp = v
		}
	}

	return stat
}

func validateInterfaces(filter string) error {
	if filter == "" {
		return nil
	}

	for _, name := range strings.Split(filter, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		_, err := net.InterfaceByName(name)
		if err != nil {
			return fmt.Errorf("interface not found: %s", name)
		}
	}

	return nil
}

func isValidInterface(name string) bool {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return false
	}

	if iface.Flags&net.FlagUp == 0 {
		return false
	}

	if iface.Flags&net.FlagLoopback != 0 {
		return false
	}

	addrs, err := iface.Addrs()
	if err != nil || len(addrs) == 0 {
		return false
	}

	return true
}

func getInterfaceSpeed(iface string) float64 {
    path := "/sys/class/net/" + iface + "/speed"

    data, err := ioutil.ReadFile(path)
    if err != nil {
        return 0
    }

    v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
    if err != nil || v <= 0 {
        return 0
    }

    return v // Mbps
}

func getUtil(mbps float64, iface string) float64 {
    speed := ifaceSpeed[iface]

    if speed <= 0 {
        return 0
    }

    return (mbps / speed) * 100
}

func getTerminalHeight() int {
	type winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}

	ws := &winsize{}

	_, _, err := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(syscall.Stdout),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(ws)),
	)

	if err != 0 {
		return 24 // fallback
	}

	return int(ws.Row)
}

func parseFilter(s string) map[string]bool {
	m := make(map[string]bool)
	if s == "" {
		return m
	}
	for _, v := range strings.Split(s, ",") {
		m[strings.TrimSpace(v)] = true
	}
	return m
}
