package main

type Message struct {
	ID          string
	From        string
	To          string
	Subject     string
	Date        string
	Body        string
	IsNew       bool
	Attachments []string
}
