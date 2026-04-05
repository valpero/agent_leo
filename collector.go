package main

import (
	"log"
	"sort"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// Metrics is the full snapshot pushed to the API
type Metrics struct {
	// CPU
	CPUPct    float64   `json:"cpu_pct"`
	CPUPerCore []float64 `json:"cpu_per_core,omitempty"`
	Load1m    float64   `json:"load_1m"`
	Load5m    float64   `json:"load_5m"`
	Load15m   float64   `json:"load_15m"`

	// Memory
	RAMUsedMB      int     `json:"ram_used_mb"`
	RAMTotalMB     int     `json:"ram_total_mb"`
	RAMAvailableMB int     `json:"ram_available_mb"`
	SwapUsedMB     int     `json:"swap_used_mb"`
	SwapTotalMB    int     `json:"swap_total_mb"`

	// Disk
	DiskMetrics []DiskMetric `json:"disk_metrics,omitempty"`

	// Network
	NetMetrics []NetMetric `json:"net_metrics,omitempty"`

	// Processes (top 15 by CPU)
	Processes []ProcessInfo `json:"processes,omitempty"`

	// Docker containers (optional)
	DockerContainers []DockerContainer `json:"docker_containers,omitempty"`

	// Temperature
	TempCelsius *float64 `json:"temp_celsius,omitempty"`

	// System
	UptimeSeconds int `json:"uptime_seconds"`
}

type DiskMetric struct {
	Mount     string  `json:"mount"`
	UsedPct   float64 `json:"used_pct"`
	UsedGB    float64 `json:"used_gb"`
	TotalGB   float64 `json:"total_gb"`
	ReadBps   float64 `json:"read_bps"`
	WriteBps  float64 `json:"write_bps"`
}

type NetMetric struct {
	Iface   string  `json:"iface"`
	RxBps   float64 `json:"rx_bps"`
	TxBps   float64 `json:"tx_bps"`
	RxTotal uint64  `json:"rx_total"`
	TxTotal uint64  `json:"tx_total"`
}

type ProcessInfo struct {
	PID    int32   `json:"pid"`
	Name   string  `json:"name"`
	CPUPct float64 `json:"cpu_pct"`
	RAMMb  uint64  `json:"ram_mb"`
	Status string  `json:"status"`
}

// IO snapshots for delta calculation
var (
	lastDiskIO map[string]disk.IOCountersStat
	lastNetIO  map[string]net.IOCountersStat
	lastIOTime time.Time
)

func Collect() (*Metrics, error) {
	m := &Metrics{}
	now := time.Now()

	// ── CPU ──────────────────────────────────────────────
	perCPU, err := cpu.Percent(500*time.Millisecond, true)
	if err == nil && len(perCPU) > 0 {
		m.CPUPerCore = perCPU
		var sum float64
		for _, v := range perCPU {
			sum += v
		}
		m.CPUPct = round2(sum / float64(len(perCPU)))
	}

	// ── Load average ──────────────────────────────────────
	if avg, err := load.Avg(); err == nil {
		m.Load1m = round2(avg.Load1)
		m.Load5m = round2(avg.Load5)
		m.Load15m = round2(avg.Load15)
	}

	// ── Memory ───────────────────────────────────────────
	if vm, err := mem.VirtualMemory(); err == nil {
		m.RAMUsedMB = int(vm.Used / 1024 / 1024)
		m.RAMTotalMB = int(vm.Total / 1024 / 1024)
		m.RAMAvailableMB = int(vm.Available / 1024 / 1024)
	}
	if sw, err := mem.SwapMemory(); err == nil {
		m.SwapUsedMB = int(sw.Used / 1024 / 1024)
		m.SwapTotalMB = int(sw.Total / 1024 / 1024)
	}

	// ── Disk usage ───────────────────────────────────────
	parts, _ := disk.Partitions(false)
	diskIONow, _ := disk.IOCounters()
	elapsed := now.Sub(lastIOTime).Seconds()
	if elapsed < 0.1 {
		elapsed = 30
	}

	seen := map[string]bool{}
	for _, p := range parts {
		// Skip pseudo-filesystems
		if skipFS(p.Fstype) || seen[p.Mountpoint] {
			continue
		}
		seen[p.Mountpoint] = true
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}
		dm := DiskMetric{
			Mount:   p.Mountpoint,
			UsedPct: round2(usage.UsedPercent),
			UsedGB:  round2(float64(usage.Used) / 1e9),
			TotalGB: round2(float64(usage.Total) / 1e9),
		}
		// IO delta
		if lastDiskIO != nil {
			dev := deviceForMount(p.Device)
			if prev, ok := lastDiskIO[dev]; ok {
				if cur, ok := diskIONow[dev]; ok {
					dm.ReadBps = round2(float64(cur.ReadBytes-prev.ReadBytes) / elapsed)
					dm.WriteBps = round2(float64(cur.WriteBytes-prev.WriteBytes) / elapsed)
				}
			}
		}
		m.DiskMetrics = append(m.DiskMetrics, dm)
	}
	lastDiskIO = diskIONow

	// ── Network ───────────────────────────────────────────
	netIONow, _ := net.IOCounters(true)
	netMap := map[string]net.IOCountersStat{}
	for _, n := range netIONow {
		netMap[n.Name] = n
	}
	for _, n := range netIONow {
		if n.Name == "lo" {
			continue
		}
		nm := NetMetric{
			Iface:   n.Name,
			RxTotal: n.BytesRecv,
			TxTotal: n.BytesSent,
		}
		if lastNetIO != nil {
			if prev, ok := lastNetIO[n.Name]; ok {
				nm.RxBps = round2(float64(n.BytesRecv-prev.BytesRecv) / elapsed)
				nm.TxBps = round2(float64(n.BytesSent-prev.BytesSent) / elapsed)
			}
		}
		m.NetMetrics = append(m.NetMetrics, nm)
	}
	lastNetIO = netMap
	lastIOTime = now

	// ── Processes (top 15 by CPU) ─────────────────────────
	procs, err := process.Processes()
	if err == nil {
		type procEntry struct {
			p      *process.Process
			cpuPct float64
		}
		var entries []procEntry
		for _, p := range procs {
			pct, err := p.CPUPercent()
			if err != nil {
				continue
			}
			entries = append(entries, procEntry{p, pct})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].cpuPct > entries[j].cpuPct
		})
		limit := 15
		if len(entries) < limit {
			limit = len(entries)
		}
		for _, e := range entries[:limit] {
			name, _ := e.p.Name()
			mi, _ := e.p.MemoryInfo()
			status, _ := e.p.Status()
			st := "running"
			if len(status) > 0 {
				st = status[0]
			}
			pi := ProcessInfo{
				PID:    e.p.Pid,
				Name:   name,
				CPUPct: round2(e.cpuPct),
				Status: st,
			}
			if mi != nil {
				pi.RAMMb = mi.RSS / 1024 / 1024
			}
			m.Processes = append(m.Processes, pi)
		}
	}

	// ── Docker containers ─────────────────────────────────
	containers, err := CollectDocker()
	if err != nil {
		log.Printf("[valpero-agent] Docker unavailable: %v", err)
	} else {
		m.DockerContainers = containers
	}

	// ── CPU temperature ───────────────────────────────────
	if temps, err := host.SensorsTemperatures(); err == nil {
		for _, t := range temps {
			if t.SensorKey == "coretemp_core0_input" || t.SensorKey == "k10temp_tctl" {
				v := round2(t.Temperature)
				m.TempCelsius = &v
				break
			}
		}
		// fallback: any CPU sensor
		if m.TempCelsius == nil {
			for _, t := range temps {
				if t.Temperature > 20 && t.Temperature < 110 {
					v := round2(t.Temperature)
					m.TempCelsius = &v
					break
				}
			}
		}
	}

	// ── Uptime ────────────────────────────────────────────
	if info, err := host.Info(); err == nil {
		m.UptimeSeconds = int(info.Uptime)
	}

	return m, nil
}

// SystemInfo is sent once on registration
type SystemInfo struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Kernel   string `json:"kernel"`
	CPUModel string `json:"cpu_model,omitempty"`
	CPUCores int    `json:"cpu_cores,omitempty"`
}

func GetSystemInfo() (*SystemInfo, error) {
	info, err := host.Info()
	if err != nil {
		return nil, err
	}
	si := &SystemInfo{
		Hostname: info.Hostname,
		OS:       info.OS + " " + info.PlatformVersion,
		Arch:     info.KernelArch,
		Kernel:   info.KernelVersion,
	}
	if cpus, err := cpu.Info(); err == nil && len(cpus) > 0 {
		si.CPUModel = cpus[0].ModelName
		si.CPUCores = int(cpus[0].Cores) * len(cpus)
	}
	return si, nil
}

func skipFS(fstype string) bool {
	skip := map[string]bool{
		"tmpfs": true, "devtmpfs": true, "devfs": true, "overlay": true,
		"squashfs": true, "proc": true, "sysfs": true, "cgroup": true,
		"cgroup2": true, "pstore": true, "securityfs": true, "debugfs": true,
		"hugetlbfs": true, "mqueue": true, "tracefs": true, "bpf": true,
		"nsfs": true, "ramfs": true, "efivarfs": true,
	}
	return skip[fstype]
}

func deviceForMount(device string) string {
	// /dev/sda1 -> sda1
	for i := len(device) - 1; i >= 0; i-- {
		if device[i] == '/' {
			return device[i+1:]
		}
	}
	return device
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
