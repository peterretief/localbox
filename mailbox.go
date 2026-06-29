package main

import (
	"log"
	"os"
	"path/filepath"
	"sort"
)

func listMessages(maildir string) ([]*Message, error) {
	var messages []*Message
	for _, dir := range []string{"new", "cur"} {
		isNew := dir == "new"
		path := filepath.Join(maildir, dir)
		entries, err := os.ReadDir(path)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}

			msg, err := parseMessage(filepath.Join(path, e.Name()), isNew)
			if err != nil {
				log.Printf("parse error: %v", err)
				continue
			}
			messages = append(messages, msg)
		}
	}

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ID > messages[j].ID
	})

	return messages, nil
}

func markAsRead(maildir, id string) {
	src := filepath.Join(maildir, "new", id)
	dst := filepath.Join(maildir, "cur", id)
	os.Rename(src, dst)
}
