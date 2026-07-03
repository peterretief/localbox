package main

type Message struct {
	ID          string
	Folder      string
	Link        string
	From        string
	To          string
	Subject     string
	Date        string
	Body        string
	IsNew       bool
	BackURL     string
	ReplyURL    string
	Attachments []string
}
