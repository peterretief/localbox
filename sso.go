package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const localBoxSSOExpiry = 2 * time.Minute

type localBoxSSOAssertion struct {
	Mailbox string `json:"mailbox"`
	Exp     int64  `json:"exp"`
	Nonce   string `json:"nonce,omitempty"`
}

func mailAppHandler(w http.ResponseWriter, r *http.Request) {
	s := requireMailboxSession(w, r)
	if s == nil {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	assertion, err := newLocalBoxSSOAssertion(s.User + "@" + s.Domain)
	if err != nil {
		log.Printf("mail handoff not configured: %v", err)
		http.Error(w, "Mail handoff is not configured", http.StatusServiceUnavailable)
		return
	}

	target := localBoxWebmailBaseURL(s.Domain) + "/localbox/sso?token=" + url.QueryEscape(assertion)
	http.Redirect(w, r, target, http.StatusFound)
}

func newLocalBoxSSOAssertion(mailbox string) (string, error) {
	secret, err := readLocalBoxSSOSecret()
	if err != nil {
		return "", err
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	payload, err := json.Marshal(localBoxSSOAssertion{
		Mailbox: strings.ToLower(strings.TrimSpace(mailbox)),
		Exp:     time.Now().Add(localBoxSSOExpiry).Unix(),
		Nonce:   base64.RawURLEncoding.EncodeToString(nonceBytes),
	})
	if err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func readLocalBoxSSOSecret() (string, error) {
	if secret := strings.TrimSpace(os.Getenv("LOCALBOX_SSO_SECRET")); secret != "" {
		return secret, nil
	}
	data, err := os.ReadFile(localBoxSSOSecretFile)
	if err != nil {
		return "", fmt.Errorf("read LocalBox SSO secret: %w", err)
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("LocalBox SSO secret is empty")
	}
	return secret, nil
}

func localBoxWebmailBaseURL(domain string) string {
	domain = normalizeDomain(domain)
	if safeDomain(domain) {
		return "https://mail." + domain
	}
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("LOCALBOX_WEBMAIL_URL")), "/"); base != "" {
		return base
	}
	return "https://mail.localboxmail.co.za"
}
