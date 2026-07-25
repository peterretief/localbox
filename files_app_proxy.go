package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func filesAppHandler(w http.ResponseWriter, r *http.Request) {
	s := requireMailboxSession(w, r)
	if s == nil {
		return
	}

	if r.URL.Path == "/apps/files" {
		http.Redirect(w, r, "/apps/files/", http.StatusFound)
		return
	}

	target, err := url.Parse("http://127.0.0.1:8082")
	if err != nil {
		http.Error(w, "File manager is not configured", http.StatusServiceUnavailable)
		return
	}

	mailbox := strings.ToLower(strings.TrimSpace(s.User + "@" + s.Domain))
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.Header.Del("X-Remote-User")
		req.Header.Set("X-Remote-User", mailbox)
		req.Header.Set("X-Forwarded-Host", r.Host)
		req.Header.Set("X-Forwarded-Proto", forwardedProto(r))
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "File manager is unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func forwardedProto(r *http.Request) string {
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto == "http" || proto == "https" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
