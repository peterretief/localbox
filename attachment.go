package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func attachmentHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	messageID := r.URL.Query().Get("id")
	filename := r.URL.Query().Get("file")
	if messageID == "" || filename == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}

	filename = filepath.Base(filename)
	messageID = filepath.Base(messageID)
	path := filepath.Join(messageAttachmentDir(s.Domain, s.User, messageID), filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.Error(w, "Attachment not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	http.ServeFile(w, r, path)
}

func saveAttachments(domain, user, messageID string, r io.Reader, contentType string) error {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil
	}

	mr := multipart.NewReader(r, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		disposition, dParams, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		if disposition != "attachment" {
			continue
		}

		filename := dParams["filename"]
		if filename == "" {
			filename = "unnamed"
		}
		filename = filepath.Base(filename)

		attDir := messageAttachmentDir(domain, user, messageID)
		if err := os.MkdirAll(attDir, 0770); err != nil {
			return err
		}

		cte := p.Header.Get("Content-Transfer-Encoding")
		var reader io.Reader = p
		if strings.ToLower(cte) == "base64" {
			reader = base64.NewDecoder(base64.StdEncoding, p)
		}

		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(attDir, filename), data, 0660); err != nil {
			return err
		}

		log.Printf("saved attachment: %s/%s", messageID, filename)
	}

	return nil
}
