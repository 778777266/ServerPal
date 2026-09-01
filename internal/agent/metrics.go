package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type MetricsCollector struct {
	mu        sync.Mutex
	lastTotal uint64
	lastIdle  uint64
	diskPath  string
}

func NewMetricsCollector(diskPath string) *MetricsCollector {
	return &MetricsCollector{diskPath: diskPath}
}

func (m *MetricsCollector) Collect() Metrics {
	result := Metrics{CPUCores: runtime.NumCPU()}
	result.CPUPercent = m.cpuPercent()
	result.Load1, result.Load5, result.Load15 = readLoad()
	result.MemoryUsed, result.MemoryTotal, result.MemoryPercent = readMemory()
	result.DiskUsed, result.DiskTotal, result.DiskPercent = readDisk(m.diskPath)
	result.UptimeSeconds = readUptime()
	return result
}

func (m *MetricsCollector) cpuPercent() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}
	var total uint64
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0
		}
		values = append(values, value)
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	previousTotal, previousIdle := m.lastTotal, m.lastIdle
	m.lastTotal, m.lastIdle = total, idle
	if previousTotal == 0 || total <= previousTotal {
		return 0
	}
	deltaTotal := total - previousTotal
	deltaIdle := idle - previousIdle
	return clampPercent(float64(deltaTotal-deltaIdle) * 100 / float64(deltaTotal))
}

func readLoad() (float64, float64, float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	return parseFloat(fields[0]), parseFloat(fields[1]), parseFloat(fields[2])
}

func readMemory() (used, total uint64, percent float64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, 0
	}
	defer f.Close()
	values := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, _ := strconv.ParseUint(fields[1], 10, 64)
		values[strings.TrimSuffix(fields[0], ":")] = value * 1024
	}
	total = values["MemTotal"]
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	if total > available {
		used = total - available
	}
	if total > 0 {
		percent = clampPercent(float64(used) * 100 / float64(total))
	}
	return
}

func readDisk(path string) (used, total uint64, percent float64) {
	if path == "" {
		path = "/"
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0
	}
	total = uint64(stat.Blocks) * uint64(stat.Bsize)
	free := uint64(stat.Bavail) * uint64(stat.Bsize)
	if total > free {
		used = total - free
	}
	if total > 0 {
		percent = clampPercent(float64(used) * 100 / float64(total))
	}
	return
}

func readUptime() uint64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[0], 64)
	if value < 0 {
		return 0
	}
	return uint64(value)
}

func parseFloat(value string) float64 {
	result, _ := strconv.ParseFloat(value, 64)
	return result
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
