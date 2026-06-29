package main

import (
	"crypto/sha512"
	"encoding/base64"
	"log"
	"os"
	"strings"
)

type User struct {
	Username string
	Domain   string
	Hash     string
}

func loadUsers() ([]User, error) {
	data, err := os.ReadFile(usersFile)
	if err != nil {
		return nil, err
	}

	var users []User
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}

		users = append(users, User{
			Username: parts[0],
			Domain:   parts[1],
			Hash:     parts[2],
		})
	}

	return users, nil
}

func hashPassword(password string) string {
	h := sha512.Sum512([]byte(password))
	return base64.StdEncoding.EncodeToString(h[:])
}

func authenticate(username, domain, password string) bool {
	users, err := loadUsers()
	if err != nil {
		log.Printf("error loading users: %v", err)
		return false
	}

	hash := hashPassword(password)
	for _, u := range users {
		if u.Username == username && u.Domain == domain && u.Hash == hash {
			return true
		}
	}

	return false
}
