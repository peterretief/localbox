package main

import (
	"fmt"
	"html"
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

func saveGeneratedDesignHTML(domain, request string) (string, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return "", fmt.Errorf("request is empty")
	}
	name, _, err := saveUploadedSiteHTML(domain, "ai-generated-page.html", []byte(renderGeneratedDesignHTML(domain, request)))
	return name, err
}

func renderGeneratedDesignHTML(domain, request string) string {
	brief := designBriefFromRequest(request)
	if brief == "" {
		brief = request
	}
	escapedDomain := html.EscapeString(domain)
	escapedBrief := html.EscapeString(brief)
	paragraphs := designBriefParagraphs(brief)
	if len(paragraphs) == 0 {
		paragraphs = []string{"Tell visitors who you are, what you do, and how to contact you."}
	}
	var body strings.Builder
	for _, paragraph := range paragraphs {
		fmt.Fprintf(&body, "<p>%s</p>\n", html.EscapeString(paragraph))
	}
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>` + escapedDomain + `</title>
<style>
:root{color-scheme:light dark;--bg:#f7f5f0;--ink:#1f2933;--muted:#667085;--card:#fff;--line:#dedbd2;--accent:#007c89}
@media(prefers-color-scheme:dark){:root{--bg:#111827;--ink:#f9fafb;--muted:#c6ccd5;--card:#172033;--line:#2c374a;--accent:#65d6e6}}
*{box-sizing:border-box}
body{font-family:Inter,Arial,sans-serif;margin:0;background:var(--bg);color:var(--ink);line-height:1.65}
a{color:var(--accent)}
.wrap{max-width:820px;margin:0 auto;padding:0 22px}
header{padding:28px 0 18px}
nav{display:flex;justify-content:space-between;gap:18px;align-items:center;border-bottom:1px solid var(--line);padding-bottom:16px}
.brand{font-weight:800;color:var(--ink);text-decoration:none}
.links{display:flex;gap:14px;flex-wrap:wrap}
.links a{color:var(--muted);text-decoration:none}
.hero{padding:54px 0 30px}
.eyebrow{color:var(--accent);font-weight:800;text-transform:uppercase;letter-spacing:.06em;font-size:13px;margin:0 0 12px}
h1{font-size:clamp(34px,7vw,58px);line-height:1.02;margin:0 0 18px;letter-spacing:0}
.intro{font-size:20px;color:var(--muted);max-width:690px}
section{padding:24px 0}
.card{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:26px}
h2{font-size:22px;margin:0 0 14px}
.actions{display:flex;gap:12px;flex-wrap:wrap;margin-top:22px}
.button{display:inline-flex;align-items:center;min-height:42px;padding:0 16px;border-radius:7px;background:var(--accent);color:#fff;text-decoration:none;font-weight:800}
.button.secondary{background:transparent;color:var(--accent);border:1px solid var(--accent)}
form{display:grid;gap:12px;margin-top:18px}
label{display:grid;gap:6px;font-weight:700}
input,textarea{width:100%;padding:11px 12px;border:1px solid var(--line);border-radius:6px;background:var(--bg);color:var(--ink);font:inherit}
textarea{min-height:130px}
footer{color:var(--muted);font-size:14px;padding:34px 0}
</style>
</head>
<body>
<header class="wrap">
<nav>
<a class="brand" href="/">` + escapedDomain + `</a>
<div class="links"><a href="#about">About</a><a href="#contact">Contact</a></div>
</nav>
</header>
<main class="wrap">
<section class="hero">
<p class="eyebrow">Online presence</p>
<h1>` + escapedDomain + `</h1>
<p class="intro">A simple, trustworthy website draft generated from your LocalBox design brief.</p>
<div class="actions"><a class="button" href="/contact.html">Contact us</a><a class="button secondary" href="#about">Learn more</a></div>
</section>
<section id="about">
<div class="card">
<h2>About</h2>
` + body.String() + `
</div>
</section>
<section id="contact">
<div class="card">
<h2>Contact</h2>
<p>Use this form for questions, bookings, or quote requests.</p>
<form method="POST" action="/contact">
<label>Name<input name="name" autocomplete="name"></label>
<label>Email<input name="email" type="email" autocomplete="email"></label>
<label>Phone<input name="phone" autocomplete="tel"></label>
<label>Message<textarea name="message">` + escapedBrief + `</textarea></label>
<button class="button" type="submit">Send message</button>
</form>
</div>
</section>
</main>
<footer class="wrap">Draft page for ` + escapedDomain + `. Review and edit before publishing.</footer>
</body>
</html>
`
}

func designBriefFromRequest(request string) string {
	marker := "from this brief:\n\n"
	start := strings.Index(request, marker)
	if start == -1 {
		return ""
	}
	start += len(marker)
	end := strings.Index(request[start:], "\n\nRequirements:")
	if end == -1 {
		return strings.TrimSpace(request[start:])
	}
	return strings.TrimSpace(request[start : start+end])
}

func designBriefParagraphs(brief string) []string {
	var paragraphs []string
	for _, line := range strings.Split(brief, "\n") {
		line = cleanDesignAssistantText(line)
		if line != "" {
			paragraphs = append(paragraphs, line)
		}
		if len(paragraphs) >= 5 {
			break
		}
	}
	return paragraphs
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
