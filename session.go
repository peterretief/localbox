package main

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

type Session struct {
	User     string
	Domain   string
	Currency string
	Expires  time.Time
}

var (
	sessions   = map[string]*Session{}
	sessionsMu sync.RWMutex
)

func newSession(user, domain string) string {
	return newSessionWithCurrency(user, domain, "")
}

func newSessionWithCurrency(user, domain, currency string) string {
	b := make([]byte, 32)
	rand.Read(b)
	token := base64.URLEncoding.EncodeToString(b)

	sessionsMu.Lock()
	sessions[token] = &Session{
		User:     user,
		Domain:   domain,
		Currency: cleanCurrency(currency),
		Expires:  time.Now().Add(24 * time.Hour),
	}
	sessionsMu.Unlock()

	return token
}

func getSession(r *http.Request) *Session {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}

	sessionsMu.RLock()
	s := sessions[c.Value]
	sessionsMu.RUnlock()
	if s == nil || time.Now().After(s.Expires) {
		return nil
	}

	return s
}

func deleteSession(r *http.Request) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return
	}

	sessionsMu.Lock()
	delete(sessions, c.Value)
	sessionsMu.Unlock()
}
