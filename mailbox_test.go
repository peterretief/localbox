package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testRawMail = []byte("From: sender@example.net\r\nTo: info@example.co.za\r\nSubject: Test\r\nDate: Tue, 14 Jul 2026 10:00:00 +0200\r\n\r\nHello\r\n")

func TestEnsureMailFolderCreatesMaildirFolder(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	if err := ensureMailFolder("example.co.za", "info", "Clients"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"new", "cur", "tmp"} {
		if _, err := os.Stat(filepath.Join(maildirPath("example.co.za", "info"), ".Clients", dir)); err != nil {
			t.Fatalf("expected folder dir %s: %v", dir, err)
		}
	}
}

func TestMoveMessageToArchive(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	id, err := writeRawToMaildir(maildirPath("example.co.za", "info"), testRawMail, time.Unix(10, 0), "test", "new")
	if err != nil {
		t.Fatal(err)
	}
	if err := moveMessageToFolder("example.co.za", "info", "", id, archiveFolderName); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(maildirPath("example.co.za", "info"), "new", id)); !os.IsNotExist(err) {
		t.Fatalf("expected original message to be moved, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(maildirPath("example.co.za", "info"), ".Archive", "cur", id)); err != nil {
		t.Fatalf("expected archived message: %v", err)
	}
}

func TestDeleteMessageMovesToTrashThenDeletesPermanently(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	id, err := writeRawToMaildir(maildirPath("example.co.za", "info"), testRawMail, time.Unix(11, 0), "test", "cur")
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteMessage("example.co.za", "info", "", id); err != nil {
		t.Fatal(err)
	}
	trashPath := filepath.Join(maildirPath("example.co.za", "info"), ".Trash", "cur", id)
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("expected message in trash: %v", err)
	}
	if err := deleteMessage("example.co.za", "info", trashFolderName, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(trashPath); !os.IsNotExist(err) {
		t.Fatalf("expected message to be permanently deleted, stat err=%v", err)
	}
}

func TestMoveMessageToCustomFolder(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	if err := ensureMailFolder("example.co.za", "info", "Clients"); err != nil {
		t.Fatal(err)
	}
	id, err := writeRawToMaildir(maildirPath("example.co.za", "info"), testRawMail, time.Unix(12, 0), "test", "cur")
	if err != nil {
		t.Fatal(err)
	}
	if err := moveMessageToFolder("example.co.za", "info", "", id, "Clients"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(maildirPath("example.co.za", "info"), ".Clients", "cur", id)); err != nil {
		t.Fatalf("expected message in custom folder: %v", err)
	}
}

func TestMessageMoveFoldersExcludesCurrentFolder(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	if err := ensureMailFolder("example.co.za", "info", "Clients"); err != nil {
		t.Fatal(err)
	}
	folders := messageMoveFolders("example.co.za", "info", "Clients")
	for _, folder := range folders {
		if folder.Name == "Clients" {
			t.Fatalf("current folder should not be a move target: %#v", folders)
		}
	}
	if len(folders) == 0 {
		t.Fatal("expected at least archive/trash move targets")
	}
}
