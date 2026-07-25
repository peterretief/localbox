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
	designAssistantMaxBrief = 4000
	designDraftMaxBytes     = 30000
)

var designDraftNamePattern = regexp.MustCompile(`^draft-\d{8}T\d{6}Z\.md$`)

type DesignAssistantRequest struct {
	Brief string
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
	if req.Brief == "" {
		return "", fmt.Errorf("describe the website you want")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# AI Website Request for %s\n\n", domain)
	fmt.Fprintf(&b, "Copy the request below into ChatGPT or another web page AI designer. Ask it to return one complete static HTML file named `index.html`.\n\n")
	fmt.Fprintf(&b, "## Request\n\n")
	fmt.Fprintf(&b, "Create a polished, responsive, single-file static HTML website for `%s` from this brief:\n\n", domain)
	fmt.Fprintf(&b, "%s\n\n", req.Brief)
	fmt.Fprintf(&b, "\nRequirements:\n\n")
	fmt.Fprintf(&b, "- Return one complete static HTML file. Use `index.html` for the homepage, or the requested filename such as `about.html`, `services.html`, or `pricing.html` for an extra page.\n")
	fmt.Fprintf(&b, "- Put all CSS inside the HTML file in a `<style>` block.\n")
	fmt.Fprintf(&b, "- Do not require a build step, package manager, framework, database, server code, or external JavaScript.\n")
	fmt.Fprintf(&b, "- Use semantic HTML, good mobile layout, readable contrast, and accessible form labels.\n")
	fmt.Fprintf(&b, "- Include a clear hero section, service/product sections, trust/help text, and a final call to action.\n")
	fmt.Fprintf(&b, "- Add a visible link to `/contact.html` for contact or quote requests.\n")
	fmt.Fprintf(&b, "- If you include a contact form, set `method=\"POST\" action=\"/contact\"` and use fields named `name`, `email`, `phone`, and `message`.\n")
	fmt.Fprintf(&b, "- Do not include analytics, tracking pixels, third-party scripts, or payment code.\n")
	fmt.Fprintf(&b, "- Use relative links only. Do not link to admin, email, files, or private LocalBox pages.\n")
	fmt.Fprintf(&b, "- Make the page feel trustworthy and people-first, not like a generic advert.\n\n")
	fmt.Fprintf(&b, "Style continuity for future page requests:\n\n")
	fmt.Fprintf(&b, "- If this is an extra page for an existing site, match the current homepage style closely: colors, typography, button style, header/nav/footer, spacing, border radius, image treatment, and mobile layout.\n")
	fmt.Fprintf(&b, "- Keep navigation consistent across pages. Use relative links such as `/`, `/about.html`, `/services.html`, and `/contact.html`.\n")
	fmt.Fprintf(&b, "- If a current homepage style reference is included below, treat it as the style source and create the new page in the same design language without copying unrelated text.\n\n")
	if style := currentHomepageStyleReference(domain); style != "" {
		fmt.Fprintf(&b, "## Current Homepage Style Reference\n\n")
		fmt.Fprintf(&b, "Use this automatically included homepage excerpt to match the site's style. Do not copy its business text unless it belongs in shared navigation or footer.\n\n")
		fmt.Fprintf(&b, "```html\n%s\n```\n\n", style)
	}
	fmt.Fprintf(&b, "Suggested visual direction if the brief does not specify one:\n\n")
	fmt.Fprintf(&b, "- Use a calm, minimalist online-presence layout with a narrow readable content column, simple top navigation, strong first image or portrait area, short introduction, services/projects/work sections, contact links, generous spacing, subtle borders, mobile-first responsiveness, and optional light/dark theme controls.\n")
	fmt.Fprintf(&b, "- Do not copy any named person's content, branding, code, external assets, analytics, framework output, or generated build paths.\n\n")
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
	req.Brief = cleanDesignAssistantMultiline(req.Brief)
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

func saveDesignDraft(domain, content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("draft is empty")
	}
	if len(content) > designDraftMaxBytes {
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

func currentHomepageStyleReference(domain string) string {
	data, err := os.ReadFile(homeHTMLPath(domain))
	if err != nil {
		return ""
	}
	source := string(data)
	var parts []string
	for _, tag := range []string{"style", "header", "nav", "footer"} {
		if section := firstHTMLSection(source, tag); section != "" {
			parts = append(parts, section)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, firstRunes(source, 5000))
	}
	return firstRunes(strings.Join(parts, "\n\n"), 9000)
}

func firstHTMLSection(source, tag string) string {
	lower := strings.ToLower(source)
	open := strings.Index(lower, "<"+tag)
	if open == -1 {
		return ""
	}
	closeMarker := "</" + tag + ">"
	close := strings.Index(lower[open:], closeMarker)
	if close == -1 {
		return firstRunes(source[open:], 2500)
	}
	close += open + len(closeMarker)
	return firstRunes(strings.TrimSpace(source[open:close]), 5000)
}

func firstRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n..."
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

func deleteDesignDraft(domain, name string) error {
	if !designDraftNamePattern.MatchString(name) {
		return fmt.Errorf("invalid draft")
	}
	return os.Remove(filepath.Join(designAssistantDraftsPath(domain), name))
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
