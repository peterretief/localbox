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
	Style        string
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
		return "No external AI provider is called here, so generating requests costs nothing."
	}
	return "Disabled."
}

func designAssistantDraftsPath(domain string) string {
	return filepath.Join(domainSiteBase(domain), "drafts")
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

	goal := req.Goal
	if goal == "" {
		goal = "Create a complete static homepage"
	}
	style := designAssistantStyleBrief(req.Style)

	var b strings.Builder
	fmt.Fprintf(&b, "# AI Website Request for %s\n\n", req.BusinessName)
	fmt.Fprintf(&b, "Copy the request below into an online AI website generator. Ask it to return one complete static HTML file named `index.html`.\n\n")
	fmt.Fprintf(&b, "## Request\n\n")
	fmt.Fprintf(&b, "Create a polished, responsive, single-file static HTML website for `%s`.\n\n", req.BusinessName)
	fmt.Fprintf(&b, "Goal: %s.\n\n", goal)
	fmt.Fprintf(&b, "Business type: %s.\n\n", req.BusinessType)
	fmt.Fprintf(&b, "Location/community: %s.\n\n", location)
	if req.Tone != "" {
		fmt.Fprintf(&b, "Tone: %s.\n\n", req.Tone)
	}
	if style != "" {
		fmt.Fprintf(&b, "Visual direction: %s\n\n", style)
	}
	fmt.Fprintf(&b, "Primary call to action: %s.\n\n", callToAction)
	fmt.Fprintf(&b, "Contact details or instruction: %s.\n\n", contact)
	fmt.Fprintf(&b, "Services/products to include:\n\n")
	for _, service := range services {
		fmt.Fprintf(&b, "- %s\n", service)
	}
	fmt.Fprintf(&b, "\nRequirements:\n\n")
	fmt.Fprintf(&b, "- Return only one complete `index.html` file.\n")
	fmt.Fprintf(&b, "- Put all CSS inside the HTML file in a `<style>` block.\n")
	fmt.Fprintf(&b, "- Do not require a build step, package manager, framework, database, server code, or external JavaScript.\n")
	fmt.Fprintf(&b, "- Use semantic HTML, good mobile layout, readable contrast, and accessible form labels.\n")
	fmt.Fprintf(&b, "- Include a clear hero section, service/product sections, trust/help text, and a final call to action.\n")
	fmt.Fprintf(&b, "- Add a visible link to `/contact.html` for contact or quote requests.\n")
	fmt.Fprintf(&b, "- If you include a contact form, set `method=\"POST\" action=\"/contact\"` and use fields named `name`, `email`, `phone`, and `message`.\n")
	fmt.Fprintf(&b, "- Do not include analytics, tracking pixels, third-party scripts, or payment code.\n")
	fmt.Fprintf(&b, "- Use relative links only. Do not link to admin, email, files, or private LocalBox pages.\n")
	fmt.Fprintf(&b, "- Make the page feel trustworthy and people-first, not like a generic advert.\n\n")
	fmt.Fprintf(&b, "## Upload Instructions\n\n")
	fmt.Fprintf(&b, "1. Save the generated file as `index.html` on your computer.\n")
	fmt.Fprintf(&b, "2. In LocalBox, open `Dashboard` -> `Home page` -> `HTML site`.\n")
	fmt.Fprintf(&b, "3. Use `Upload HTML`, choose `index.html`, and upload it.\n")
	fmt.Fprintf(&b, "4. LocalBox will publish `index.html` as the active homepage.\n")
	fmt.Fprintf(&b, "5. Click `View active page` and check the public site.\n")
	fmt.Fprintf(&b, "6. If it looks wrong, upload a corrected `index.html`; the previous homepage remains listed in HTML pages when available.\n\n")
	fmt.Fprintf(&b, "_This request was generated locally by LocalBox for %s. It has not changed the live website._\n", domain)

	content := b.String()
	return content, nil
}

func cleanDesignAssistantRequest(req DesignAssistantRequest) DesignAssistantRequest {
	req.Goal = cleanDesignAssistantText(req.Goal)
	req.Style = cleanDesignAssistantText(req.Style)
	req.BusinessName = cleanDesignAssistantText(req.BusinessName)
	req.BusinessType = cleanDesignAssistantText(req.BusinessType)
	req.Location = cleanDesignAssistantText(req.Location)
	req.Services = cleanDesignAssistantMultiline(req.Services)
	req.Tone = cleanDesignAssistantText(req.Tone)
	req.Contact = cleanDesignAssistantText(req.Contact)
	req.CallToAction = cleanDesignAssistantText(req.CallToAction)
	return req
}

func designAssistantStyleBrief(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "online-presence":
		return "Use a calm, minimalist online-presence layout: narrow readable content column, simple top navigation, strong first image or portrait area, short introduction, services/projects/work sections, contact links, generous spacing, subtle borders, mobile-first responsiveness, and optional light/dark theme controls. Do not copy any named person's content, branding, code, external assets, analytics, framework output, or generated build paths."
	default:
		return ""
	}
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
