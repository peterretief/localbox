package main

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	primaryLocalBoxHost    = "localboxmail.co.za"
	sessionHandoffParam    = "localbox_handoff"
	sessionHandoffLifetime = 2 * time.Minute
)

type sessionHandoff struct {
	User     string
	Domain   string
	Currency string
	Expires  time.Time
}

var (
	sessionHandoffs   = map[string]sessionHandoff{}
	sessionHandoffsMu sync.Mutex
)

func canonicalHostMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if consumeSessionHandoff(w, r) {
			return
		}
		if shouldRedirectToSessionDomain(r) {
			redirectWithSessionHandoff(w, r, getSession(r), safeRedirectPath(r.URL.RequestURI()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func shouldRedirectToSessionDomain(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	host := requestHost(r)
	if host != primaryLocalBoxHost {
		return false
	}
	s := getSession(r)
	return s != nil && safeDomain(s.Domain) && s.Domain != primaryLocalBoxHost
}

func redirectWithSessionHandoff(w http.ResponseWriter, r *http.Request, s *Session, nextPath string) {
	if s == nil || !safeDomain(s.Domain) || s.Domain == primaryLocalBoxHost {
		if nextPath == "" {
			nextPath = "/dashboard"
		}
		http.Redirect(w, r, nextPath, http.StatusFound)
		return
	}
	token := newSessionHandoff(s)
	if nextPath == "" {
		nextPath = "/dashboard"
	}
	target := "https://" + s.Domain + addHandoffParam(nextPath, token)
	http.Redirect(w, r, target, http.StatusFound)
}

func newSessionHandoff(s *Session) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)

	sessionHandoffsMu.Lock()
	sessionHandoffs[token] = sessionHandoff{
		User:     s.User,
		Domain:   s.Domain,
		Currency: s.Currency,
		Expires:  time.Now().Add(sessionHandoffLifetime),
	}
	sessionHandoffsMu.Unlock()
	return token
}

func consumeSessionHandoff(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimSpace(r.URL.Query().Get(sessionHandoffParam))
	if token == "" {
		return false
	}

	sessionHandoffsMu.Lock()
	handoff, ok := sessionHandoffs[token]
	delete(sessionHandoffs, token)
	sessionHandoffsMu.Unlock()

	if !ok || time.Now().After(handoff.Expires) || handoff.Domain != requestHost(r) {
		http.Error(w, "Invalid session handoff", http.StatusForbidden)
		return true
	}

	sessionToken := newSessionWithCurrency(handoff.User, handoff.Domain, handoff.Currency)
	setSessionCookie(w, r, sessionToken)
	http.Redirect(w, r, removeHandoffParam(r.URL), http.StatusFound)
	return true
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   forwardedProto(r) == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

func addHandoffParam(path, token string) string {
	u, err := url.Parse(path)
	if err != nil {
		return "/dashboard?" + sessionHandoffParam + "=" + url.QueryEscape(token)
	}
	q := u.Query()
	q.Set(sessionHandoffParam, token)
	u.RawQuery = q.Encode()
	return u.String()
}

func removeHandoffParam(u *url.URL) string {
	clean := *u
	q := clean.Query()
	q.Del(sessionHandoffParam)
	clean.RawQuery = q.Encode()
	if clean.Path == "" {
		clean.Path = "/dashboard"
	}
	return clean.RequestURI()
}

func requestHost(r *http.Request) string {
	host := strings.ToLower(strings.TrimSpace(r.Host))
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.ToLower(forwardedHost)
	}
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	return host
}
