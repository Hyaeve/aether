package app

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Store) event(level, module, message string) {
	switch level {
	case "success":
		level = "info"
	case "cancelled", "interrupted":
		level = "warn"
	case "info", "warn", "error", "debug":
	default:
		level = "info"
	}
	entry := LogEntry{Time: time.Now(), Level: level, Module: module, Message: message}
	data, _ := json.Marshal(entry)
	fmt.Fprintln(os.Stdout, string(data))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Logs = append(s.state.Logs, entry)
	s.trimLogsLocked()
	if err := s.persistLogsLocked(); err != nil {
		fmt.Fprintln(os.Stderr, "Aether: failed to persist system log")
	}
}

func (s *Store) initLogs(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, "system.json"))
	if err == nil {
		if err := json.Unmarshal(data, &s.state.Logs); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	s.logDir = dir
	s.trimLogsLocked()
	if err := s.persistLogsLocked(); err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *Store) trimLogsLocked() {
	days, max := s.state.Settings.LogDays, s.state.Settings.LogMaxEntries
	if days <= 0 {
		days = 15
	}
	if max <= 0 {
		max = 20000
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	kept := s.state.Logs[:0]
	for _, entry := range s.state.Logs {
		if !entry.Time.Before(cutoff) {
			kept = append(kept, entry)
		}
	}
	if len(kept) > max {
		kept = kept[len(kept)-max:]
	}
	s.state.Logs = kept
}

func (s *Store) persistLogsLocked() error {
	if s.logDir == "" {
		return s.saveLocked()
	}
	data, err := json.Marshal(s.state.Logs)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.logDir, "system.json"), data)
}

func (s *Store) pruneLogs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trimLogsLocked()
	if err := s.persistLogsLocked(); err != nil {
		fmt.Fprintln(os.Stderr, "Aether: failed to prune system log")
	}
}

// Reading logs must not rewrite the log file or serialize unrelated configuration.
func (s *Store) logSnapshot() []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	days, max := s.state.Settings.LogDays, s.state.Settings.LogMaxEntries
	if days <= 0 {
		days = 15
	}
	if max <= 0 {
		max = 20000
	}
	entries := s.state.Logs
	if len(entries) > max {
		entries = entries[len(entries)-max:]
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	result := make([]LogEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.Time.Before(cutoff) {
			result = append(result, entry)
		}
	}
	return result
}

func logModule(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/auth/"), path == "/api/account":
		return "audit"
	case path == "/api/files" || strings.HasPrefix(path, "/api/files/"):
		return "files"
	case strings.HasPrefix(path, "/api/storages"), strings.HasPrefix(path, "/api/webdav"):
		return "storage"
	case strings.HasPrefix(path, "/api/tasks"), strings.HasPrefix(path, "/api/cas"):
		return "tasks"
	case strings.HasPrefix(path, "/api/links"), path == "/api/link-playback":
		return "links"
	default:
		return "system"
	}
}

type auditResponse struct {
	http.ResponseWriter
	status int
}

func (w *auditResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *auditResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *auditResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Resolve from local interfaces, never an untrusted request Host header.
func defaultPublicURL() string {
	addr, err := listenAddress()
	if err != nil {
		addr = ":15151"
	}
	host, port, _ := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
		host = "127.0.0.1"
		addrs, _ := net.InterfaceAddrs()
		for _, address := range addrs {
			if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil && network.IP.IsGlobalUnicast() {
				host = network.IP.String()
				if network.IP.IsPrivate() {
					break
				}
			}
		}
	}
	return "http://" + net.JoinHostPort(host, port)
}
