package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	minPasswordLength      = 6
	passwordHashAlgorithm  = "pbkdf2-sha256"
	passwordHashIterations = 120000
	passwordSaltBytes      = 16
	passwordKeyBytes       = 32
)

func passwordMinLengthError() error {
	return fmt.Errorf("password must be at least %d characters", minPasswordLength)
}

func newPasswordMinLengthError() string {
	return fmt.Sprintf("New password must be at least %d characters.", minPasswordLength)
}

type User struct {
	Username      string
	Domain        string
	Hash          string
	RecoveryEmail string
}

func loadUsers() ([]User, error) {
	data, err := os.ReadFile(usersFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var users []User
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 3 {
			continue
		}

		users = append(users, User{
			Username:      parts[0],
			Domain:        parts[1],
			Hash:          parts[2],
			RecoveryEmail: userField(parts, 3),
		})
	}

	return users, nil
}

func hashPassword(password string) string {
	h := sha512.Sum512([]byte(password))
	return base64.StdEncoding.EncodeToString(h[:])
}

func hashPasswordStrong(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	key := pbkdf2SHA256([]byte(password), salt, passwordHashIterations, passwordKeyBytes)
	return fmt.Sprintf(
		"%s$%d$%s$%s",
		passwordHashAlgorithm,
		passwordHashIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func verifyPassword(password, storedHash string) bool {
	parts := strings.Split(storedHash, "$")
	if len(parts) == 4 && parts[0] == passwordHashAlgorithm {
		iterations, err := strconv.Atoi(parts[1])
		if err != nil || iterations <= 0 {
			return false
		}

		salt, err := base64.RawStdEncoding.DecodeString(parts[2])
		if err != nil {
			return false
		}
		expected, err := base64.RawStdEncoding.DecodeString(parts[3])
		if err != nil {
			return false
		}

		actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
		return subtle.ConstantTimeCompare(actual, expected) == 1
	}

	return subtle.ConstantTimeCompare([]byte(hashPassword(password)), []byte(storedHash)) == 1
}

func authenticate(username, domain, password string) bool {
	users, err := loadUsers()
	if err != nil {
		log.Printf("error loading users: %v", err)
		return false
	}

	for _, u := range users {
		if u.Username == username && u.Domain == domain && verifyPassword(password, u.Hash) {
			if passwordNeedsRehash(u.Hash) {
				if err := setPasswordHash(username, domain, password); err != nil {
					log.Printf("could not update password hash parameters: %v", err)
				}
			}
			return true
		}
	}

	return false
}

func authenticatedMailboxDomains(username, password string) []string {
	username = normalizeMailboxUser(username)
	if !safeMailboxUser(username) {
		return nil
	}
	users, err := loadUsers()
	if err != nil {
		log.Printf("error loading users: %v", err)
		return nil
	}
	var domains []string
	for _, u := range users {
		if u.Username != username || !verifyPassword(password, u.Hash) {
			continue
		}
		if passwordNeedsRehash(u.Hash) {
			if err := setPasswordHash(username, u.Domain, password); err != nil {
				log.Printf("could not update password hash parameters: %v", err)
			}
		}
		domains = append(domains, u.Domain)
	}
	return domains
}

func mailboxExists(username, domain string) bool {
	username = normalizeMailboxUser(username)
	domain = normalizeDomain(domain)
	users, err := loadUsers()
	if err != nil {
		log.Printf("error loading users: %v", err)
		return false
	}
	for _, user := range users {
		if user.Username == username && user.Domain == domain {
			return true
		}
	}
	return false
}

func changePassword(username, domain, currentPassword, newPassword string) error {
	users, err := loadUsers()
	if err != nil {
		return err
	}

	for i, u := range users {
		if u.Username != username || u.Domain != domain {
			continue
		}
		if !verifyPassword(currentPassword, u.Hash) {
			return fmt.Errorf("current password is incorrect")
		}

		if err := setPasswordHashAt(users, i, newPassword); err != nil {
			return err
		}
		if err := syncExternalMailboxPassword(username, domain, newPassword); err != nil {
			return err
		}
		return syncMailboxSystem()
	}

	return fmt.Errorf("user not found")
}

func setPasswordHash(username, domain, password string) error {
	users, err := loadUsers()
	if err != nil {
		return err
	}

	for i, u := range users {
		if u.Username == username && u.Domain == domain {
			return setPasswordHashAt(users, i, password)
		}
	}

	return fmt.Errorf("user not found")
}

func setPasswordHashAt(users []User, index int, password string) error {
	hash, err := hashPasswordStrong(password)
	if err != nil {
		return err
	}
	users[index].Hash = hash
	return saveUsers(users)
}

func passwordNeedsRehash(storedHash string) bool {
	parts := strings.Split(storedHash, "$")
	if len(parts) != 4 || parts[0] != passwordHashAlgorithm {
		return true
	}

	iterations, err := strconv.Atoi(parts[1])
	return err != nil || iterations != passwordHashIterations
}

func saveUsers(users []User) error {
	var b bytes.Buffer
	for _, u := range users {
		if strings.TrimSpace(u.RecoveryEmail) == "" {
			fmt.Fprintf(&b, "%s:%s:%s\n", u.Username, u.Domain, u.Hash)
			continue
		}
		fmt.Fprintf(&b, "%s:%s:%s:%s\n", u.Username, u.Domain, u.Hash, strings.ToLower(strings.TrimSpace(u.RecoveryEmail)))
	}

	return os.WriteFile(usersFile, b.Bytes(), 0640)
}

func userField(parts []string, index int) string {
	if len(parts) <= index {
		return ""
	}
	return strings.TrimSpace(parts[index])
}

func syncDovecotMailboxPassword(username, domain, password string) error {
	if _, err := os.Stat(dovecotUsersFile); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	hash, err := dovecotPasswordHash(password)
	if err != nil {
		return err
	}
	line, err := dovecotUserLine(username, domain, hash)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(dovecotUsersFile)
	if err != nil {
		return err
	}

	address := username + "@" + domain
	var b bytes.Buffer
	replaced := false
	for _, existing := range strings.Split(string(data), "\n") {
		existing = strings.TrimSpace(existing)
		if existing == "" {
			continue
		}
		if strings.HasPrefix(existing, address+":") {
			fmt.Fprintln(&b, line)
			replaced = true
			continue
		}
		fmt.Fprintln(&b, existing)
	}
	if !replaced {
		fmt.Fprintln(&b, line)
	}

	return os.WriteFile(dovecotUsersFile, b.Bytes(), 0644)
}

func syncExternalMailboxPassword(username, domain, password string) error {
	if err := syncDovecotMailboxPassword(username, domain, password); err != nil {
		return err
	}
	if radicaleUsesDovecotAuth() {
		return nil
	}
	return syncRadicalePassword(username, domain, password)
}

func syncMailboxSystem() error {
	helper := "/usr/local/sbin/localbox-sync-mailmaps"
	if _, err := os.Stat(helper); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cmd := exec.Command(helper)
	if os.Geteuid() != 0 {
		cmd = exec.Command("sudo", "-n", helper)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not sync LocalBox mail maps: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func radicaleUsesDovecotAuth() bool {
	data, err := os.ReadFile(radicaleConfigFile)
	if err != nil {
		return false
	}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if section == "auth" && strings.EqualFold(strings.TrimSpace(parts[0]), "type") {
			return strings.EqualFold(strings.TrimSpace(parts[1]), "dovecot")
		}
	}
	return false
}

func syncRadicalePassword(username, domain, password string) error {
	if _, err := os.Stat(radicaleUsersFile); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	hash, err := sha512CryptHash(password)
	if err != nil {
		return err
	}
	line := username + "@" + domain + ":" + hash

	data, err := os.ReadFile(radicaleUsersFile)
	if err != nil {
		return err
	}

	address := strings.ToLower(username + "@" + domain)
	var b bytes.Buffer
	replaced := false
	for _, existing := range strings.Split(string(data), "\n") {
		existing = strings.TrimSpace(existing)
		if existing == "" {
			continue
		}
		parts := strings.SplitN(existing, ":", 2)
		if strings.ToLower(parts[0]) == address {
			fmt.Fprintln(&b, line)
			replaced = true
			continue
		}
		fmt.Fprintln(&b, existing)
	}
	if !replaced {
		fmt.Fprintln(&b, line)
	}

	return os.WriteFile(radicaleUsersFile, b.Bytes(), 0640)
}

func dovecotPasswordHash(password string) (string, error) {
	return sha512CryptHash(password)
}

func sha512CryptHash(password string) (string, error) {
	cmd := exec.Command("openssl", "passwd", "-6", "-stdin")
	cmd.Stdin = strings.NewReader(password)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("could not create sha512-crypt password hash: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func dovecotUserLine(username, domain, hash string) (string, error) {
	www, err := user.Lookup("www")
	if err != nil {
		return "", fmt.Errorf("could not find www user for Dovecot password file: %w", err)
	}
	mail, err := user.LookupGroup("mail")
	if err != nil {
		return "", fmt.Errorf("could not find mail group for Dovecot password file: %w", err)
	}

	home := filepath.Join(localBoxBase, "domains", domain, "mail", username)
	return fmt.Sprintf("%s@%s:%s:%s:%s::%s::", username, domain, hash, www.Uid, mail.Gid, home), nil
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	hashLength := sha256.Size
	numBlocks := (keyLength + hashLength - 1) / hashLength
	output := make([]byte, 0, numBlocks*hashLength)

	for block := 1; block <= numBlocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		var blockBytes [4]byte
		binary.BigEndian.PutUint32(blockBytes[:], uint32(block))
		mac.Write(blockBytes[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)

		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}

		output = append(output, t...)
	}

	return output[:keyLength]
}
