package app

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
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
	if err := s.update(func(st *State) error {
		st.Logs = append(st.Logs, entry)
		if len(st.Logs) > 2000 {
			st.Logs = st.Logs[len(st.Logs)-2000:]
		}
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "Aether: failed to persist system log")
	}
}

func logModule(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/auth/"), path == "/api/account":
		return "audit"
	case path == "/api/files":
		return "files"
	case strings.HasPrefix(path, "/api/storages"), strings.HasPrefix(path, "/api/webdav"):
		return "storage"
	case strings.HasPrefix(path, "/api/tasks"), strings.HasPrefix(path, "/api/cas"):
		return "tasks"
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
