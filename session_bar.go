package main

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"text/template"
)

type bufferedResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{header: make(http.Header)}
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func sessionBarMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := newBufferedResponseWriter()
		next.ServeHTTP(rec, r)

		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		body := rec.body.Bytes()
		if shouldInjectSessionBar(r, rec.header, status, body) {
			body = injectLoggedInSessionBar(body, getSession(r))
			rec.header.Del("Content-Length")
		} else if rec.header.Get("Content-Length") == "" && len(body) > 0 {
			rec.header.Set("Content-Length", strconv.Itoa(len(body)))
		}

		copyHeader(w.Header(), rec.header)
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

func shouldInjectSessionBar(r *http.Request, header http.Header, status int, body []byte) bool {
	if r.Method == http.MethodHead || status != http.StatusOK || getSession(r) == nil || len(body) == 0 {
		return false
	}
	contentType := strings.ToLower(header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "text/html") {
		return false
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "<html") &&
		strings.Contains(lower, "<body") &&
		!strings.Contains(lower, "localbox-session-bar")
}

func injectLoggedInSessionBar(data []byte, s *Session) []byte {
	if s == nil {
		return data
	}
	htmlText := string(data)
	lower := strings.ToLower(htmlText)
	i := strings.Index(lower, "<body")
	if i < 0 {
		return data
	}
	end := strings.Index(lower[i:], ">")
	if end < 0 {
		return data
	}
	insert := i + end + 1
	bar := loggedInSessionBarHTML(s)
	return []byte(htmlText[:insert] + bar + htmlText[insert:])
}

func loggedInSessionBarHTML(s *Session) string {
	label := strings.TrimSpace(s.User)
	if userExists(s.User, s.Domain) {
		label = strings.TrimSpace(s.User + "@" + s.Domain)
		return `<div class="localbox-session-bar" style="box-sizing:border-box;width:100%;background:#1a1a2e;color:#fff;padding:10px 18px;font:14px Arial,sans-serif;display:flex;gap:14px;align-items:center;justify-content:flex-end;position:relative;z-index:9999;"><span>Logged in as ` + template.HTMLEscapeString(label) + `</span><span>Type: ` + template.HTMLEscapeString(sessionUserType(s)) + `</span><a href="/dashboard" style="color:#fff;text-decoration:underline;">Dashboard</a><a href="/logout" style="color:#fff;text-decoration:underline;">Sign out</a></div>`
	}
	return `<div class="localbox-session-bar" style="box-sizing:border-box;width:100%;background:#1a1a2e;color:#fff;padding:10px 18px;font:14px Arial,sans-serif;display:flex;gap:14px;align-items:center;justify-content:flex-end;position:relative;z-index:9999;"><span>Logged in as ` + template.HTMLEscapeString(label) + `</span><span>Type: ` + template.HTMLEscapeString(sessionUserType(s)) + `</span><a href="/account/domains" style="color:#fff;text-decoration:underline;">My domains</a><a href="/public/logout" style="color:#fff;text-decoration:underline;">Sign out</a></div>`
}

func sessionUserType(s *Session) string {
	if s == nil {
		return ""
	}
	switch {
	case isBoxAdmin(s.User, s.Domain):
		return "box admin"
	case isDomainAdmin(s.User, s.Domain):
		return "domain admin"
	case userExists(s.User, s.Domain):
		return "mailbox user"
	case publicUserExists(s.User, s.Domain):
		return "customer"
	default:
		return "user"
	}
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
