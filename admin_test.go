package main

import (
	"path/filepath"
	"testing"
)

func TestAdminViewIncludesRegisteredPublicUsers(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })

	if err := registerPublicUserProfile("example.com", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street, Cape Town", "secret-password", "USD"); err != nil {
		t.Fatal(err)
	}
	if err := registerPublicUserProfile("other.com", "other", "other@example.net", "Other Example", "+27 82 123 4567", "2 Example Street, Cape Town", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}

	view := adminView("example.com")
	if len(view.RegisteredUsers) != 1 {
		t.Fatalf("expected one registered user for example.com, got %d", len(view.RegisteredUsers))
	}
	user := view.RegisteredUsers[0]
	if user.Username != "buyer" || user.Email != "buyer@example.net" || user.FullName != "Buyer Example" || user.Currency != "USD" {
		t.Fatalf("unexpected registered user: %#v", user)
	}
}
