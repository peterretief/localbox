package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	archiveFolderName = "Archive"
	trashFolderName   = "Trash"
	sentFolderName    = "sent"
)

var mailFolderPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,31}$`)

type MailFolder struct {
	Name   string
	URL    string
	Active bool
}

func listMessages(maildir string) ([]*Message, error) {
	return listMessagesInFolder(maildir, "")
}

func listMessagesInFolder(maildir, folder string) ([]*Message, error) {
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
			msg.Folder = folder
			if folder == "" {
				msg.Link = "/message?id=" + msg.ID
			} else {
				msg.Link = "/message?folder=" + folder + "&id=" + msg.ID
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

func cleanMailFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", nil
	}
	if strings.EqualFold(name, sentFolderName) {
		return sentFolderName, nil
	}
	if strings.EqualFold(name, archiveFolderName) {
		return archiveFolderName, nil
	}
	if strings.EqualFold(name, trashFolderName) {
		return trashFolderName, nil
	}
	if !mailFolderPattern.MatchString(name) {
		return "", fmt.Errorf("folder names can use letters, numbers, spaces, dots, dashes, or underscores")
	}
	return name, nil
}

func mailboxFolderPath(domain, user, folder string) (string, error) {
	folder, err := cleanMailFolderName(folder)
	if err != nil {
		return "", err
	}
	switch folder {
	case "":
		return maildirPath(domain, user), nil
	case sentFolderName:
		return sentMaildirPath(domain, user), nil
	default:
		return filepath.Join(maildirPath(domain, user), "."+folder), nil
	}
}

func ensureMailFolder(domain, user, folder string) error {
	path, err := mailboxFolderPath(domain, user, folder)
	if err != nil {
		return err
	}
	if folder == "" {
		return nil
	}
	for _, dir := range []string{"new", "cur", "tmp"} {
		if err := os.MkdirAll(filepath.Join(path, dir), 0770); err != nil {
			return err
		}
	}
	return nil
}

func listMailFolders(domain, user, active, basePath string) []MailFolder {
	seen := map[string]bool{}
	names := []string{archiveFolderName, trashFolderName}
	seen[archiveFolderName] = true
	seen[trashFolderName] = true

	entries, err := os.ReadDir(maildirPath(domain, user))
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			name := strings.TrimPrefix(entry.Name(), ".")
			clean, err := cleanMailFolderName(name)
			if err != nil || clean == "" || clean == sentFolderName || seen[clean] {
				continue
			}
			if _, err := os.Stat(filepath.Join(maildirPath(domain, user), entry.Name(), "cur")); err != nil {
				continue
			}
			names = append(names, clean)
			seen[clean] = true
		}
	}
	sort.Strings(names)

	var folders []MailFolder
	for _, name := range names {
		folders = append(folders, MailFolder{
			Name:   name,
			URL:    inboxFolderURLForBase(basePath, name, 1),
			Active: name == active,
		})
	}
	return folders
}

func findMessagePath(domain, user, folder, id string) (string, error) {
	if id == "" || id != filepath.Base(id) {
		return "", fmt.Errorf("invalid message")
	}
	maildir, err := mailboxFolderPath(domain, user, folder)
	if err != nil {
		return "", err
	}
	for _, dir := range []string{"new", "cur"} {
		path := filepath.Join(maildir, dir, id)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	if folder == sentFolderName {
		for _, dir := range []string{"new", "cur"} {
			path := filepath.Join(legacySentMaildirPath(domain, user), dir, id)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("message not found")
}

func moveMessageToFolder(domain, user, fromFolder, id, toFolder string) error {
	src, err := findMessagePath(domain, user, fromFolder, id)
	if err != nil {
		return err
	}
	if err := ensureMailFolder(domain, user, toFolder); err != nil {
		return err
	}
	dstBase, err := mailboxFolderPath(domain, user, toFolder)
	if err != nil {
		return err
	}
	dst := filepath.Join(dstBase, "cur", filepath.Base(src))
	if _, err := os.Stat(dst); err == nil {
		dst = filepath.Join(dstBase, "cur", uniqueMaildirName(filepath.Base(src)))
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	return nil
}

func deleteMessage(domain, user, folder, id string) error {
	src, err := findMessagePath(domain, user, folder, id)
	if err != nil {
		return err
	}
	if folder == trashFolderName {
		if err := os.Remove(src); err != nil {
			return err
		}
		_ = os.RemoveAll(messageAttachmentDir(domain, user, filepath.Base(src)))
		return nil
	}
	return moveMessageToFolder(domain, user, folder, id, trashFolderName)
}

func uniqueMaildirName(name string) string {
	return fmt.Sprintf("%s.%d", name, os.Getpid())
}
