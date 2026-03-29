package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"vsc-taskrunner/internal/uiconfig"
)

type MetricsSnapshot struct {
	UpdatedAt string         `json:"updatedAt"`
	CPU       CPUMetric      `json:"cpu"`
	Memory    MemoryMetric   `json:"memory"`
	Storage   StorageMetric  `json:"storage"`
}

type CPUMetric struct {
	Percent   float64 `json:"percent"`
	Timestamp string  `json:"timestamp"`
}

type MemoryMetric struct {
	Current MemoryPoint   `json:"current"`
	History []MemoryPoint `json:"history"`
}

type MemoryPoint struct {
	Timestamp   string  `json:"timestamp"`
	UsedBytes   uint64  `json:"usedBytes"`
	TotalBytes  uint64  `json:"totalBytes"`
	UsedPercent float64 `json:"usedPercent"`
}

type StorageMetric struct {
	Timestamp     string `json:"timestamp"`
	HistoryBytes  uint64 `json:"historyBytes"`
	ArtifactBytes uint64 `json:"artifactBytes"`
	WorktreeBytes uint64 `json:"worktreeBytes"`
	FreeBytes     uint64 `json:"freeBytes"`
}

type metricsMessage struct {
	eventType string
	data      []byte
}

type cpuCounters struct {
	idle  uint64
	total uint64
}

type MetricsService struct {
	config     uiconfig.MetricsConfig
	historyDir string

	mu       sync.RWMutex
	snapshot MetricsSnapshot
	prevCPU  *cpuCounters
	subs     []chan metricsMessage
}

func NewMetricsService(config uiconfig.MetricsConfig, historyDir string) *MetricsService {
	if !config.Enabled {
		return nil
	}
	service := &MetricsService{
		config:     config,
		historyDir: historyDir,
	}
	service.collectCPU()
	service.collectMemory()
	service.collectStorage()
	go service.runCPU()
	go service.runMemory()
	go service.runStorage()
	return service
}

func (s *MetricsService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)

	if raw, err := json.Marshal(s.Snapshot()); err == nil {
		writeSSEEvent(w, rc, "snapshot", raw)
	}

	ch := s.subscribe()
	defer s.unsubscribe(ch)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case message, open := <-ch:
			if !open {
				return
			}
			writeSSEEvent(w, rc, message.eventType, message.data)
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			_ = rc.Flush()
		}
	}
}

func (s *MetricsService) Snapshot() MetricsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := s.snapshot
	snapshot.Memory.History = append([]MemoryPoint(nil), s.snapshot.Memory.History...)
	return snapshot
}

func (s *MetricsService) subscribe() chan metricsMessage {
	ch := make(chan metricsMessage, 16)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, ch)
	return ch
}

func (s *MetricsService) unsubscribe(target chan metricsMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, ch := range s.subs {
		if ch == target {
			s.subs = append(s.subs[:index], s.subs[index+1:]...)
			close(ch)
			return
		}
	}
}

func (s *MetricsService) runCPU() {
	ticker := time.NewTicker(time.Duration(s.config.CPUInterval) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.collectCPU()
	}
}

func (s *MetricsService) runMemory() {
	ticker := time.NewTicker(time.Duration(s.config.MemoryInterval) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.collectMemory()
	}
}

func (s *MetricsService) runStorage() {
	ticker := time.NewTicker(time.Duration(s.config.StorageInterval) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.collectStorage()
	}
}

func (s *MetricsService) collectCPU() {
	current, err := sampleCPUCounters()
	if err != nil {
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	percent := s.snapshot.CPU.Percent
	if s.prevCPU != nil {
		totalDelta := current.total - s.prevCPU.total
		idleDelta := current.idle - s.prevCPU.idle
		if totalDelta > 0 {
			percent = (1 - float64(idleDelta)/float64(totalDelta)) * 100
		}
	}
	s.prevCPU = current
	s.snapshot.CPU = CPUMetric{
		Percent:   percent,
		Timestamp: now.Format(time.RFC3339Nano),
	}
	s.snapshot.UpdatedAt = now.Format(time.RFC3339Nano)
	s.broadcastLocked("metrics")
}

func (s *MetricsService) collectMemory() {
	usedBytes, totalBytes, err := sampleMemoryUsage()
	if err != nil || totalBytes == 0 {
		return
	}
	now := time.Now().UTC()
	point := MemoryPoint{
		Timestamp:   now.Format(time.RFC3339Nano),
		UsedBytes:   usedBytes,
		TotalBytes:  totalBytes,
		UsedPercent: (float64(usedBytes) / float64(totalBytes)) * 100,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Memory.Current = point
	s.snapshot.Memory.History = append(s.snapshot.Memory.History, point)
	cutoff := now.Add(-time.Duration(s.config.MemoryHistoryWindow) * time.Second)
	filtered := s.snapshot.Memory.History[:0]
	for _, item := range s.snapshot.Memory.History {
		ts, err := time.Parse(time.RFC3339Nano, item.Timestamp)
		if err == nil && ts.Before(cutoff) {
			continue
		}
		filtered = append(filtered, item)
	}
	s.snapshot.Memory.History = append([]MemoryPoint(nil), filtered...)
	s.snapshot.UpdatedAt = now.Format(time.RFC3339Nano)
	s.broadcastLocked("metrics")
}

func (s *MetricsService) collectStorage() {
	historyBytes, artifactBytes, worktreeBytes := uint64(0), uint64(0), uint64(0)
	root := s.historyDir
	if root != "" {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			size := uint64(info.Size())
			historyBytes += size
			switch {
			case strings.Contains(filepath.ToSlash(path), "/artifacts/"):
				artifactBytes += size
			case strings.Contains(filepath.ToSlash(path), "/worktree/"):
				worktreeBytes += size
			}
			return nil
		})
	}

	freeBytes := uint64(0)
	var stat unix.Statfs_t
	if root != "" && unix.Statfs(root, &stat) == nil {
		freeBytes = stat.Bavail * uint64(stat.Bsize)
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Storage = StorageMetric{
		Timestamp:     now.Format(time.RFC3339Nano),
		HistoryBytes:  historyBytes,
		ArtifactBytes: artifactBytes,
		WorktreeBytes: worktreeBytes,
		FreeBytes:     freeBytes,
	}
	s.snapshot.UpdatedAt = now.Format(time.RFC3339Nano)
	s.broadcastLocked("metrics")
}

func (s *MetricsService) broadcastLocked(eventType string) {
	raw, err := json.Marshal(s.snapshot)
	if err != nil {
		return
	}
	for _, ch := range s.subs {
		select {
		case ch <- metricsMessage{eventType: eventType, data: raw}:
		default:
		}
	}
}

func sampleCPUCounters() (*cpuCounters, error) {
	switch runtime.GOOS {
	case "linux":
		return sampleLinuxCPUCounters()
	case "darwin":
		return sampleDarwinCPUCounters()
	default:
		return nil, fmt.Errorf("cpu metrics unsupported on %s", runtime.GOOS)
	}
}

func sampleLinuxCPUCounters() (*cpuCounters, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return nil, fmt.Errorf("cpu line not found")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return nil, fmt.Errorf("unexpected cpu stat format")
	}
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	return &cpuCounters{
		idle:  values[3],
		total: total,
	}, nil
}

func sampleDarwinCPUCounters() (*cpuCounters, error) {
	output, err := exec.Command("sysctl", "-n", "kern.cp_time").Output()
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(output))
	if len(fields) < 4 {
		return nil, fmt.Errorf("unexpected kern.cp_time format")
	}
	var total uint64
	values := make([]uint64, 0, len(fields))
	for _, field := range fields {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		total += value
	}
	return &cpuCounters{
		idle:  values[3],
		total: total,
	}, nil
}

func sampleMemoryUsage() (usedBytes uint64, totalBytes uint64, err error) {
	switch runtime.GOOS {
	case "linux":
		return sampleLinuxMemoryUsage()
	case "darwin":
		return sampleDarwinMemoryUsage()
	default:
		return 0, 0, fmt.Errorf("memory metrics unsupported on %s", runtime.GOOS)
	}
}

func sampleLinuxMemoryUsage() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	var totalKB, availableKB uint64
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalKB, _ = strconv.ParseUint(fields[1], 10, 64)
		case "MemAvailable:":
			availableKB, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}
	if totalKB == 0 {
		return 0, 0, fmt.Errorf("meminfo missing total")
	}
	totalBytes := totalKB * 1024
	usedBytes := totalBytes - (availableKB * 1024)
	return usedBytes, totalBytes, nil
}

func sampleDarwinMemoryUsage() (uint64, uint64, error) {
	totalRaw, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, 0, err
	}
	totalBytes, err := strconv.ParseUint(strings.TrimSpace(string(totalRaw)), 10, 64)
	if err != nil {
		return 0, 0, err
	}

	output, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, 0, err
	}
	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 {
		return 0, 0, fmt.Errorf("vm_stat output missing")
	}
	pageSize := uint64(4096)
	if fields := strings.Fields(lines[0]); len(fields) >= 8 {
		raw := strings.Trim(fields[7], ".")
		if value, parseErr := strconv.ParseUint(raw, 10, 64); parseErr == nil {
			pageSize = value
		}
	}
	values := map[string]uint64{}
	for _, line := range lines[1:] {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}
		raw := strings.Trim(strings.TrimSpace(parts[1]), ".")
		raw = strings.ReplaceAll(raw, ".", "")
		value, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			continue
		}
		values[parts[0]] = value
	}
	availablePages := values["Pages free"] + values["Pages inactive"] + values["Pages speculative"]
	usedBytes := totalBytes - (availablePages * pageSize)
	return usedBytes, totalBytes, nil
}
