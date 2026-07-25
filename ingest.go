package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MailPayload struct {
	To   string `json:"to"`
	From string `json:"from"`
	Raw  string `json:"raw"`
}

func ingestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Ingest-Secret") != ingestSecret {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 25<<20))
	if err != nil {
		http.Error(w, "Read error", http.StatusInternalServerError)
		return
	}

	var payload MailPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	parts := strings.SplitN(strings.ToLower(payload.To), "@", 2)
	if len(parts) != 2 {
		http.Error(w, "Invalid to address", http.StatusBadRequest)
		return
	}

	user, domain := parts[0], parts[1]
	if !safeLabel.MatchString(user) || !safeLabel.MatchString(domain) {
		http.Error(w, "Invalid address characters", http.StatusBadRequest)
		return
	}
	if !mailboxExists(user, domain) {
		http.Error(w, "Unknown mailbox", http.StatusNotFound)
		return
	}

	maildirBase := maildirPath(domain, user)
	for _, dir := range []string{"new", "cur", "tmp"} {
		if err := os.MkdirAll(filepath.Join(maildirBase, dir), 0770); err != nil {
			log.Printf("mkdir error: %v", err)
			http.Error(w, "Storage error", http.StatusInternalServerError)
			return
		}
	}

	filename := fmt.Sprintf("%d.%s.localmail", time.Now().UnixNano(), user)
	path := filepath.Join(maildirBase, "new", filename)
	if err := os.WriteFile(path, []byte(payload.Raw), 0660); err != nil {
		log.Printf("write error: %v", err)
		http.Error(w, "Write error", http.StatusInternalServerError)
		return
	}

	msgReader := strings.NewReader(payload.Raw)
	m, err := mail.ReadMessage(msgReader)
	if err == nil {
		saveAttachments(domain, user, filename, m.Body, m.Header.Get("Content-Type"))
	}

	log.Printf("delivered: %s -> %s", payload.From, payload.To)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
