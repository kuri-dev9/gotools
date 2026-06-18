package hwinfo

import (
	"fmt"
	"net"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const sectorSize = 512

// =====================
// Struct
// =====================

type CPUInfo struct {
	Model string `json:"model"`
	Cores int    `json:"cores"`
}

type MemInfo struct {
    TotalGB float64 `json:"total_gb"` // OS
    HWGB    float64 `json:"hw_gb"`    // iomem
    RsvGB   float64 `json:"rsv_gb"`   // HW - OS
}

type OSInfo struct {
	Name string `json:"name"`
}

type DiskInfo struct {
	Name   string  `json:"name"`
	SizeGB float64 `json:"size_gb"`
	Model  string  `json:"model"`
	Mount  string  `json:"mount"`
}

type NICInfo struct {
	Name  string `json:"name"`
	IP    string `json:"ip"`
	Link  string `json:"link"`
	Speed string `json:"speed"`
}

type CaptureCard struct {
	Raw string `json:"raw"`
}

type UsageInfo struct {
	CPUUsage   float64 `json:"cpu_usage"`
	Load1      float64 `json:"load1"`
	Load5      float64 `json:"load5"`
	Load15     float64 `json:"load15"`
	MemUsage   float64 `json:"mem_usage"`
	MemUsedGB  float64 `json:"mem_used_gb"`
	MemTotalGB float64 `json:"mem_total_gb"`
}

type HWInfo struct {
	CPU          *CPUInfo      `json:"cpu,omitempty"`
	Memory       *MemInfo      `json:"memory,omitempty"`
	OS           *OSInfo       `json:"os,omitempty"`
	Kernel       string        `json:"kernel,omitempty"`
	Model        string        `json:"model,omitempty"`
	Disks        []DiskInfo    `json:"disks,omitempty"`
	DiskCount    int           `json:"disk_count,omitempty"`
	NICs         []NICInfo     `json:"nics,omitempty"`
	CaptureCards []CaptureCard `json:"capture_cards,omitempty"`
	Usage        *UsageInfo    `json:"usage,omitempty"`
}

// =====================
// Collect
// =====================

func Collect(filter map[string]bool) *HWInfo {
	info := &HWInfo{}

	if filter["all"] || filter["cpu"] {
		c := GetCPU()
		info.CPU = &c
	}

	if filter["all"] || filter["mem"] {
		m := GetMem()
		info.Memory = &m
	}

	if filter["all"] || filter["os"] {
		o := GetOS()
		info.OS = &o
		info.Kernel = GetKernel()
	}

	if filter["all"] || filter["model"] {
		info.Model = GetModel()
	}

	if filter["all"] || filter["disk"] {
		disks := GetDisks()
		info.Disks = disks
		info.DiskCount = len(disks)
	}

	if filter["all"] || filter["net"] {
		info.NICs = GetNICs()
	}

	if filter["all"] || filter["pci"] {
		info.CaptureCards = GetCaptureCards()
	}

	if filter["all"] || filter["usage"] {
		u := GetUsage()
		info.Usage = &u
	}
	return info
}

// =====================
// CPU
// =====================

func GetCPU() CPUInfo {
	data, _ := ioutil.ReadFile("/proc/cpuinfo")
	lines := strings.Split(string(data), "\n")

	model := ""
	cores := 0

	for _, l := range lines {
		if strings.HasPrefix(l, "model name") && model == "" {
			model = strings.Split(l, ":")[1]
		}
		if strings.HasPrefix(l, "processor") {
			cores++
		}
	}

	return CPUInfo{
		Model: strings.TrimSpace(model),
		Cores: cores,
	}
}

// =====================
// Memory (GB)
// =====================
func GetMem() MemInfo {
    var osGB float64
    data, _ := ioutil.ReadFile("/proc/meminfo")

    for _, l := range strings.Split(string(data), "\n") {
        if strings.HasPrefix(l, "MemTotal") {
            kb, _ := strconv.ParseFloat(strings.Fields(l)[1], 64)
            osGB = kb / 1024 / 1024
            break
        }
    }

    // HW (iomem)
    var hwBytes float64
    iomem, _ := ioutil.ReadFile("/proc/iomem")
    for _, l := range strings.Split(string(iomem), "\n") {
        if strings.Contains(l, "System RAM") {
            parts := strings.Fields(l)
            if len(parts) > 0 {
                addr := strings.Split(parts[0], "-")
                if len(addr) == 2 {
                    start, _ := strconv.ParseUint(addr[0], 16, 64)
                    end, _ := strconv.ParseUint(addr[1], 16, 64)
                    hwBytes += float64(end - start + 1)
                }
            }
        }
    }

    hwGB := hwBytes / 1024 / 1024 / 1024
    rsvGB := hwGB - osGB

    return MemInfo{
        TotalGB: osGB,
        HWGB:    hwGB,
        RsvGB:   rsvGB,
    }
}

// =====================
// OS / Kernel
// =====================
func GetOS() OSInfo {

	// 1순위: os-release
	data, err := ioutil.ReadFile("/etc/os-release")
	if err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(l, "PRETTY_NAME=") {
				return OSInfo{
					Name: strings.Trim(strings.Split(l, "=")[1], `"`),
				}
			}
		}
	}

	// 2순위: centos-release
	data, err = ioutil.ReadFile("/etc/centos-release")
	if err == nil {
		return OSInfo{Name: strings.TrimSpace(string(data))}
	}

	// 3순위: redhat-release
	data, err = ioutil.ReadFile("/etc/redhat-release")
	if err == nil {
		return OSInfo{Name: strings.TrimSpace(string(data))}
	}

	return OSInfo{Name: "unknown"}
}

func GetKernel() string {
	data, _ := ioutil.ReadFile("/proc/sys/kernel/osrelease")
	return strings.TrimSpace(string(data))
}

// =====================
// Model
// =====================
func GetModel() string {
	data, err := ioutil.ReadFile("/sys/class/dmi/id/product_name")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// =====================
// Disk (GB + Physical Only)
// =====================
var systemMounts = []string{
    "/", "/boot", "/boot/efi",
    "/var", "/var/log",
    "/usr", "/bin", "/sbin",
    "/lib", "/lib64",
    "/opt", "/etc", "/tmp",
}

func isSystemMount(m string) bool {
    for _, s := range systemMounts {
        if m == s || strings.HasPrefix(m, s+"/") {
            return true
        }
    }
    return false
}

func getLabelMap() map[string]string {
	m := make(map[string]string)

	files, err := ioutil.ReadDir("/dev/disk/by-label")
	if err != nil {
		return m
	}

	for _, f := range files {
		link, err := os.Readlink("/dev/disk/by-label/" + f.Name())
		if err != nil {
			continue
		}

		dev := "/dev/" + filepath.Base(link)
		m[dev] = f.Name()
	}

	return m
}

func getMountMap() map[string]string {
	m := make(map[string]string)

	data, err := ioutil.ReadFile("/proc/mounts")
	if err != nil {
		return m
	}

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			m[fields[0]] = fields[1]
		}
	}
	return m
}

func readTrim(path string) string {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func GetDisks() []DiskInfo {
	files, _ := ioutil.ReadDir("/sys/block")

	mountMap := getMountMap()
	labelMap := getLabelMap()

	var out []DiskInfo

	for _, f := range files {
		name := f.Name()

		if strings.HasPrefix(name, "loop") ||
			strings.HasPrefix(name, "ram") {
			continue
		}

		base := "/sys/block/" + name

		// size
		sizeStr := readTrim(base + "/size")
		sectors, _ := strconv.ParseFloat(sizeStr, 64)
		gb := sectors * sectorSize / 1024 / 1024 / 1024

		// model
		model := strings.TrimSpace(
			readTrim(base+"/device/vendor") + " " +
				readTrim(base+"/device/model"),
		)

		// mount / label 찾기
		mount := ""
		label := ""

		for dev, m := range mountMap {
			if strings.HasPrefix(dev, "/dev/"+name) {
				if isSystemMount(m) {
					mount = "/root"
					break
				}
				mount = m
				label = labelMap[dev]
				break
			}
		}

		display := label
		if display == "" {
			display = mount
		}
		if display == "" {
			display = "-"
		}

		out = append(out, DiskInfo{
			Name:   name,
			SizeGB: gb,
			Model:  model,
			Mount:  display,
		})
	}

	return out
}

func SumDiskSize(disks []DiskInfo) float64 {
    var total float64
    for _, d := range disks {
        total += d.SizeGB
    }
    return total
}

// =====================
// NIC (Name + IP)
// =====================
func comma(v int) string {
    s := strconv.Itoa(v)
    n := len(s)

    if n <= 3 {
        return s
    }

    var out []byte
    pre := n % 3

    if pre > 0 {
        out = append(out, s[:pre]...)
        if n > pre {
            out = append(out, ',')
        }
    }

    for i := pre; i < n; i += 3 {
        out = append(out, s[i:i+3]...)
        if i+3 < n {
            out = append(out, ',')
        }
    }

    return string(out)
}

func formatSpeed(speed string) string {
    if speed == "" || speed == "-1" {
        return "-"
    }

    v, err := strconv.Atoi(speed)
    if err != nil {
        return speed + " Mb/s"
    }

    return fmt.Sprintf("%s Mb/s", comma(v))
}

func GetNICs() []NICInfo {
	ifaces, _ := net.Interfaces()

	var out []NICInfo

	for _, iface := range ifaces {

		if iface.Name == "lo" {
			continue
		}

		addrs, _ := iface.Addrs()

		ip := ""
		for _, a := range addrs {
			switch v := a.(type) {
			case *net.IPNet:
				if v.IP.To4() != nil {
					ip = v.IP.String()
				}
			case *net.IPAddr:
				if v.IP.To4() != nil {
					ip = v.IP.String()
				}
			}
			if ip != "" {
				break
			}
		}

		if ip == "" {
			continue
		}

		base := "/sys/class/net/" + iface.Name

		// carrier
		link := "DOWN"
		if readTrim(base+"/carrier") == "1" {
			link = "UP"
		}

		// speed
		speedRaw := readTrim(base + "/speed")
		speed := ""
		if speedRaw == "" || speedRaw == "-1" {
			speed = "-"
		} else {
			speed = formatSpeed(speedRaw)
		}

		out = append(out, NICInfo{
			Name:  iface.Name,
			IP:    ip,
			Link:  link,
			Speed: speed,
		})
	}

	return out
}

// =====================
// Capture Card
// =====================
func GetCaptureCards() []CaptureCard {
	cmd := exec.Command("lspci")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(string(out), "\n")

	var result []CaptureCard

	keywords := []string{
		"Napatech", // Na
		"Silicom",  // Sil
		"Fiber",    // Fib
	}

	for _, line := range lines {
		for _, k := range keywords {
			if strings.Contains(strings.ToLower(line), strings.ToLower(k)) {
				result = append(result, CaptureCard{Raw: line})
				break
			}
		}
	}

	return result
}

// =====================
// Usage
// =====================
type cpuSnapshot struct {
	idle   float64
	total  float64
	idleTs time.Time
}

func readCPUStat() (idle, total float64) {
	data, _ := ioutil.ReadFile("/proc/stat")
	fields := strings.Fields(strings.Split(string(data), "\n")[0])

	var vals []float64
	for i := 1; i < len(fields); i++ {
		v, _ := strconv.ParseFloat(fields[i], 64)
		vals = append(vals, v)
	}

	for _, v := range vals {
		total += v
	}

	idle = vals[3] + vals[4] // idle + iowait
	return
}

func getCpuStart() cpuSnapshot {
	idle, total := readCPUStat()
	return cpuSnapshot{idle: idle, total: total, idleTs: time.Now()}
}

func getCpuEnd(start cpuSnapshot) float64 {
	elapsed := time.Since(start.idleTs)
	if elapsed < 500*time.Millisecond {
		time.Sleep(500*time.Millisecond - elapsed)
	}

	idle2, total2 := readCPUStat()
	deltaIdle := idle2 - start.idle
	deltaTotal := total2 - start.total

	if deltaTotal == 0 {
		return 0
	}

	return (1.0 - deltaIdle/deltaTotal) * 100
}

func getLoad() (float64, float64, float64) {
	data, _ := ioutil.ReadFile("/proc/loadavg")
	fields := strings.Fields(string(data))

	l1, _ := strconv.ParseFloat(fields[0], 64)
	l5, _ := strconv.ParseFloat(fields[1], 64)
	l15, _ := strconv.ParseFloat(fields[2], 64)

	return l1, l5, l15
}

func getMemUsage() (usage float64, usedGB float64, totalGB float64) {
	data, _ := ioutil.ReadFile("/proc/meminfo")

	var total, available, free, buffers, cached float64

	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "MemTotal") {
			total, _ = strconv.ParseFloat(strings.Fields(l)[1], 64)
		}
		if strings.HasPrefix(l, "MemAvailable") {
			available, _ = strconv.ParseFloat(strings.Fields(l)[1], 64)
		}
		if strings.HasPrefix(l, "MemFree") {
			free, _ = strconv.ParseFloat(strings.Fields(l)[1], 64)
		}
		if strings.HasPrefix(l, "Buffers") {
			buffers, _ = strconv.ParseFloat(strings.Fields(l)[1], 64)
		}
		if strings.HasPrefix(l, "Cached") {
			cached, _ = strconv.ParseFloat(strings.Fields(l)[1], 64)
		}
	}

	// CentOS6 fallback
	if available == 0 {
		available = free + buffers + cached
	}

	if total == 0 {
		return 0, 0, 0
	}

	used := total - available

	// kB → GB
	totalGB = total / 1024 / 1024
	usedGB = used / 1024 / 1024

	usage = used / total * 100

	return
}

func GetUsage() UsageInfo {
	start := getCpuStart()
	load1, load5, load15 := getLoad()
	memUsage, memUsedGB, memTotalGB := getMemUsage()
	cpu := getCpuEnd(start)

	return UsageInfo{
		CPUUsage:   cpu,
		Load1:      load1,
		Load5:      load5,
		Load15:     load15,
		MemUsage:   memUsage,
		MemUsedGB:  memUsedGB,
		MemTotalGB: memTotalGB,
	}
}
