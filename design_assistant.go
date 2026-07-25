package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	designAssistantMaxField = 500
	designAssistantMaxBrief = 2400
)

var designDraftNamePattern = regexp.MustCompile(`^draft-\d{8}T\d{6}Z\.md$`)

type DesignAssistantRequest struct {
	Goal         string
	BusinessName string
	BusinessType string
	Location     string
	Services     string
	Tone         string
	Contact      string
	CallToAction string
}

type DesignDraft struct {
	Name        string
	Content     string
	Preview     string
	Created     string
	DisplaySize string
}

func designAssistantEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("LOCALBOX_DESIGN_ASSISTANT")))
	return value == "" || value == "1" || value == "true" || value == "local"
}

func designAssistantProviderName() string {
	if !designAssistantEnabled() {
		return "disabled"
	}
	return "local template"
}

func designAssistantBillingNote() string {
	if designAssistantProviderName() == "local template" {
		return "No external AI provider is called in this mode, so generating drafts costs nothing."
	}
	return "Disabled."
}

func designAssistantDraftsPath(domain string) string {
	return filepath.Join(domainSiteBase(domain), "drafts")
}

func designAssistantBackupsPath(domain string) string {
	return filepath.Join(domainSiteBase(domain), "draft-backups")
}

func generateLocalDesignDraft(domain string, req DesignAssistantRequest) (string, error) {
	if !designAssistantEnabled() {
		return "", fmt.Errorf("design assistant is disabled")
	}
	req = cleanDesignAssistantRequest(req)
	if req.BusinessName == "" {
		return "", fmt.Errorf("business name is required")
	}
	if req.BusinessType == "" {
		return "", fmt.Errorf("business type is required")
	}

	services := designAssistantLines(req.Services)
	if len(services) == 0 {
		services = []string{"A clear description of your main service", "A simple way for visitors to ask questions", "Helpful follow-up from the team"}
	}

	contact := req.Contact
	if contact == "" {
		contact = "Contact us through the form on this website."
	}
	callToAction := req.CallToAction
	if callToAction == "" {
		callToAction = "Ask for a quote"
	}
	location := req.Location
	if location == "" {
		location = "our community"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", req.BusinessName)
	fmt.Fprintf(&b, "%s helps people in %s with %s. We keep the process clear, practical, and people first.\n\n", req.BusinessName, location, strings.ToLower(req.BusinessType))
	fmt.Fprintf(&b, "## How We Can Help\n\n")
	for _, service := range services {
		fmt.Fprintf(&b, "- %s\n", service)
	}
	fmt.Fprintf(&b, "\n## What To Expect\n\n")
	fmt.Fprintf(&b, "We will listen first, answer questions in plain language, and help you choose the next step that makes sense for you.\n\n")
	if req.Tone != "" {
		fmt.Fprintf(&b, "This page should feel %s while staying easy to read.\n\n", strings.ToLower(req.Tone))
	}
	fmt.Fprintf(&b, "## %s\n\n", callToAction)
	fmt.Fprintf(&b, "%s\n\n", contact)
	fmt.Fprintf(&b, "[Contact us](/contact.html)\n\n")
	fmt.Fprintf(&b, "_Draft generated locally for %s. Review before publishing._\n", domain)

	content := b.String()
	if len(content) > designAssistantMaxBrief {
		content = content[:designAssistantMaxBrief]
	}
	return content, nil
}

func cleanDesignAssistantRequest(req DesignAssistantRequest) DesignAssistantRequest {
	req.Goal = cleanDesignAssistantText(req.Goal)
	req.BusinessName = cleanDesignAssistantText(req.BusinessName)
	req.BusinessType = cleanDesignAssistantText(req.BusinessType)
	req.Location = cleanDesignAssistantText(req.Location)
	req.Services = cleanDesignAssistantMultiline(req.Services)
	req.Tone = cleanDesignAssistantText(req.Tone)
	req.Contact = cleanDesignAssistantText(req.Contact)
	req.CallToAction = cleanDesignAssistantText(req.CallToAction)
	return req
}

func cleanDesignAssistantText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > designAssistantMaxField {
		value = strings.TrimSpace(value[:designAssistantMaxField])
	}
	return value
}

func cleanDesignAssistantMultiline(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		line = cleanDesignAssistantText(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if line != "" {
			lines = append(lines, line)
		}
	}
	value = strings.Join(lines, "\n")
	if len(value) > designAssistantMaxBrief {
		value = strings.TrimSpace(value[:designAssistantMaxBrief])
	}
	return value
}

func designAssistantLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		line = cleanDesignAssistantText(line)
		if line != "" {
			lines = append(lines, line)
		}
		if len(lines) >= 8 {
			break
		}
	}
	return lines
}

func stripDesignAssistantReviewNote(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var kept []string
	for _, line := range lines {
		if strings.Contains(line, "Draft generated locally for ") && strings.Contains(line, "Review before publishing") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n")) + "\n"
}

func saveDesignDraft(domain, content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("draft is empty")
	}
	if len(content) > 12000 {
		return "", fmt.Errorf("draft is too large")
	}
	dir := designAssistantDraftsPath(domain)
	if err := os.MkdirAll(dir, 0775); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		name := "draft-" + now.Add(time.Duration(i)*time.Second).Format("20060102T150405Z") + ".md"
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return name, writeFileAtomic(path, []byte(content+"\n"), 0664)
		}
	}
	return "", fmt.Errorf("could not allocate draft name")
}

func backupCurrentHomeMarkdown(domain string) (string, error) {
	current, err := loadHomeMarkdown(domain)
	if err != nil {
		return "", err
	}
	current = strings.TrimSpace(current)
	if current == "" {
		return "", nil
	}
	dir := designAssistantBackupsPath(domain)
	if err := os.MkdirAll(dir, 0775); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		name := "home-" + now.Add(time.Duration(i)*time.Second).Format("20060102T150405Z") + ".md"
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return name, writeFileAtomic(path, []byte(current+"\n"), 0664)
		}
	}
	return "", fmt.Errorf("could not allocate backup name")
}

func listDesignDrafts(domain string) ([]DesignDraft, error) {
	entries, err := os.ReadDir(designAssistantDraftsPath(domain))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var drafts []DesignDraft
	for _, entry := range entries {
		if entry.IsDir() || !designDraftNamePattern.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		drafts = append(drafts, DesignDraft{
			Name:        entry.Name(),
			Created:     info.ModTime().Format("2006-01-02 15:04"),
			DisplaySize: displaySize(info.Size()),
		})
	}
	sort.Slice(drafts, func(i, j int) bool {
		return drafts[i].Name > drafts[j].Name
	})
	return drafts, nil
}

func loadDesignDraft(domain, name string) (string, error) {
	if !designDraftNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid draft")
	}
	data, err := os.ReadFile(filepath.Join(designAssistantDraftsPath(domain), name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
