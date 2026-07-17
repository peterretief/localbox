package main

import (
	"encoding/json"
	"net/http"
	"os"
	"syscall"
)

type healthStatus struct {
	OK     bool     `json:"ok"`
	Checks []string `json:"checks"`
	Errors []string `json:"errors,omitempty"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := healthStatus{OK: true}
	check := func(name string, err error) {
		if err != nil {
			status.OK = false
			status.Errors = append(status.Errors, name+": "+err.Error())
			return
		}
		status.Checks = append(status.Checks, name)
	}

	check("localbox-base", requireDir(localBoxBase))
	check("domains-config", requireFile(domainsFile))
	check("users-config", requireFile(usersFile))
	check("free-space", requireFreeBytes(localBoxBase, 50<<20))

	w.Header().Set("Content-Type", "application/json")
	if !status.OK {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(status)
}

func requireDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrInvalid
	}
	return nil
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.ErrInvalid
	}
	return nil
}

func requireFreeBytes(path string, minimum uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return err
	}
	if stat.Bavail*uint64(stat.Bsize) < minimum {
		return syscall.ENOSPC
	}
	return nil
}
