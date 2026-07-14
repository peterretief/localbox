package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const inboxPageSize = 20

type InboxView struct {
	Messages      []*Message
	User          string
	Domain        string
	Query         string
	View          string
	Folder        string
	Total         int
	Page          int
	TotalPages    int
	Start         int
	End           int
	AllURL        string
	NewURL        string
	SentURL       string
	Folders       []MailFolder
	SearchURL     string
	PrevURL       string
	NextURL       string
	HasPrev       bool
	HasNext       bool
	ShowingNew    bool
	ShowingSent   bool
	ShowingFolder bool
	HasMessages   bool
	Access        AccessView
}

func newInboxView(messages []*Message, s *Session, r *http.Request) InboxView {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	view := r.URL.Query().Get("view")
	folder, _ := cleanMailFolderName(r.URL.Query().Get("folder"))
	if folder != "" {
		view = "folder"
	} else if view != "new" && view != "sent" {
		view = "all"
	}

	filtered := filterMessages(messages, query, view)
	total := len(filtered)
	totalPages := pageCount(total, inboxPageSize)
	page := parsePage(r.URL.Query().Get("page"), totalPages)
	start := (page - 1) * inboxPageSize
	end := start + inboxPageSize
	if end > total {
		end = total
	}

	pageMessages := []*Message{}
	if total > 0 {
		pageMessages = filtered[start:end]
	}

	data := InboxView{
		Messages:      pageMessages,
		User:          s.User,
		Domain:        s.Domain,
		Query:         query,
		View:          view,
		Folder:        folder,
		Total:         total,
		Page:          page,
		TotalPages:    totalPages,
		Start:         start + 1,
		End:           end,
		AllURL:        inboxURL(query, "all", 1),
		NewURL:        inboxURL(query, "new", 1),
		SentURL:       inboxURL(query, "sent", 1),
		Folders:       listMailFolders(s.Domain, s.User, folder),
		SearchURL:     inboxSearchURL(view, folder),
		HasPrev:       page > 1,
		HasNext:       page < totalPages,
		ShowingNew:    view == "new",
		ShowingSent:   view == "sent",
		ShowingFolder: view == "folder",
		HasMessages:   total > 0,
		Access:        accessView(s),
	}
	if data.HasPrev {
		data.PrevURL = inboxFolderAwareURL(query, view, folder, page-1)
	}
	if data.HasNext {
		data.NextURL = inboxFolderAwareURL(query, view, folder, page+1)
	}

	return data
}

func filterMessages(messages []*Message, query, view string) []*Message {
	query = strings.ToLower(query)
	var filtered []*Message
	for _, msg := range messages {
		if view == "new" && !msg.IsNew {
			continue
		}
		if query != "" && !messageMatches(msg, query) {
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func messageMatches(msg *Message, query string) bool {
	searchText := strings.ToLower(strings.Join([]string{
		msg.From,
		msg.To,
		msg.Subject,
		msg.Date,
		msg.Body,
	}, " "))
	return strings.Contains(searchText, query)
}

func parsePage(value string, totalPages int) int {
	page, err := strconv.Atoi(value)
	if err != nil || page < 1 {
		return 1
	}
	if page > totalPages {
		return totalPages
	}
	return page
}

func pageCount(total, pageSize int) int {
	if total == 0 {
		return 1
	}
	return (total + pageSize - 1) / pageSize
}

func inboxURL(query, view string, page int) string {
	return inboxFolderAwareURL(query, view, "", page)
}

func inboxFolderURL(folder string, page int) string {
	return inboxFolderAwareURL("", "folder", folder, page)
}

func inboxSearchURL(view, folder string) string {
	return inboxFolderAwareURL("", view, folder, 1)
}

func inboxFolderAwareURL(query, view, folder string, page int) string {
	values := url.Values{}
	if strings.TrimSpace(query) != "" {
		values.Set("q", strings.TrimSpace(query))
	}
	if folder != "" {
		values.Set("folder", folder)
	} else if view == "new" || view == "sent" {
		values.Set("view", view)
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/?" + encoded
	}
	return "/"
}
