package main

import (
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func readPart(mediaType, encoding string, r io.Reader) (string, error) {
	var reader io.Reader = r
	switch strings.ToLower(encoding) {
	case "quoted-printable":
		reader = quotedprintable.NewReader(r)
	case "base64":
		reader = base64.NewDecoder(base64.StdEncoding, r)
	}

	b, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	text := string(b)
	if mediaType == "text/html" {
		text = stripHTML(text)
	}

	return text, nil
}

func parseMultipart(boundary string, r io.Reader) (string, []string, error) {
	mr := multipart.NewReader(r, boundary)
	var plainText, htmlText string
	var attachments []string

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		ct := p.Header.Get("Content-Type")
		cte := p.Header.Get("Content-Transfer-Encoding")
		mediaType, params, _ := mime.ParseMediaType(ct)
		disposition, dParams, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		if disposition == "attachment" {
			name := dParams["filename"]
			if name == "" {
				name = "unnamed"
			}
			attachments = append(attachments, name)
			continue
		}

		if strings.HasPrefix(mediaType, "multipart/") {
			text, atts, _ := parseMultipart(params["boundary"], p)
			if plainText == "" {
				plainText = text
			}
			attachments = append(attachments, atts...)
			continue
		}

		text, _ := readPart(mediaType, cte, p)
		switch mediaType {
		case "text/plain":
			plainText = text
		case "text/html":
			if htmlText == "" {
				htmlText = stripHTML(text)
			}
		}
	}

	if plainText != "" {
		return plainText, attachments, nil
	}

	return htmlText, attachments, nil
}

func parseBody(contentType, transferEncoding string, r io.Reader) (string, []string, error) {
	if contentType == "" {
		b, err := io.ReadAll(r)
		return string(b), nil, err
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		b, err := io.ReadAll(r)
		return string(b), nil, err
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		return parseMultipart(params["boundary"], r)
	}

	body, err := readPart(mediaType, transferEncoding, r)
	return body, nil, err
}

func parseMessage(path string, isNew bool) (*Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m, err := mail.ReadMessage(f)
	if err != nil {
		return nil, err
	}

	msg := &Message{
		ID:      filepath.Base(path),
		IsNew:   isNew,
		From:    m.Header.Get("From"),
		To:      m.Header.Get("To"),
		Subject: m.Header.Get("Subject"),
		Date:    m.Header.Get("Date"),
	}
	msg.ReplyURL = replyURL(m.Header)

	body, attachments, err := parseBody(
		m.Header.Get("Content-Type"),
		m.Header.Get("Content-Transfer-Encoding"),
		m.Body,
	)
	if err != nil {
		msg.Body = "(could not parse body)"
	} else {
		msg.Body = body
		msg.Attachments = attachments
	}

	return msg, nil
}

func replyURL(header mail.Header) string {
	to := strings.TrimSpace(header.Get("Reply-To"))
	if to == "" {
		to = strings.TrimSpace(header.Get("From"))
	}
	return composeURL(to, "")
}

func composeURL(to, subject string) string {
	if parsed, err := mail.ParseAddress(to); err == nil {
		to = parsed.Address
	}

	subject = strings.TrimSpace(subject)
	if subject != "" && !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}

	values := url.Values{}
	if to != "" {
		values.Set("to", to)
	}
	if subject != "" {
		values.Set("subject", subject)
	}
	if encoded := values.Encode(); encoded != "" {
		return "/compose?" + encoded
	}
	return "/compose"
}
