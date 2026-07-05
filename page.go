package main

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const defaultHomeMarkdown = `# Welcome

This is your LocalBox home page.

- Edit this page from LocalBox Mail.
- Keep it simple.
- Link to the contact page with [Contact us](/contact.html).
`

var (
	markdownLinkedImagePattern = regexp.MustCompile(`\[!\[([^\]]*)\]\(([^\s)]+)(?:\s+"([^"]*)")?\)\]\(([^)]+)\)`)
	markdownImagePattern       = regexp.MustCompile(`!\[([^\]]*)\]\(([^\s)]+)(?:\s+"([^"]*)")?\)`)
	markdownLinkPattern        = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	homeHTMLNamePattern        = regexp.MustCompile(`^index(?:-\d{8}T\d{6}Z)?\.html$`)
	uploadedHTMLNamePattern    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?\.html$`)
)

type inlineToken struct {
	start int
	end   int
	html  string
}

type PageEditorData struct {
	Content       string
	Preview       template.HTML
	Images        []SiteImage
	HTMLFiles     []SiteHTMLFile
	Domain        string
	Mode          string
	Error         string
	Success       string
	ImageError    string
	ImageSuccess  string
	UploadError   string
	UploadSuccess string
}

type SiteHTMLFile struct {
	Name        string
	Active      bool
	Uploaded    bool
	Homepage    bool
	Size        int64
	DisplaySize string
	Modified    string
}

func loadHomeMarkdown(domain string) (string, error) {
	data, err := os.ReadFile(homeMarkdownPath(domain))
	if os.IsNotExist(err) {
		return defaultHomeMarkdown, nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func saveHomePage(domain, markdown string) error {
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		return err
	}
	if err := os.WriteFile(homeMarkdownPath(domain), []byte(markdown), 0664); err != nil {
		return err
	}
	if err := os.WriteFile(homeHTMLPath(domain), renderHomeHTML(markdown), 0664); err != nil {
		return err
	}
	return saveSiteMode(domain, "markdown")
}

func homeMarkdownPath(domain string) string {
	return filepath.Join(domainSiteBase(domain), "index.md")
}

func homeHTMLPath(domain string) string {
	return filepath.Join(domainSiteBase(domain), "index.html")
}

func siteModePath(domain string) string {
	return filepath.Join(domainSiteBase(domain), "site.conf")
}

func htmlUploadsPath(domain string) string {
	return filepath.Join(domainSiteBase(domain), "html-uploads.conf")
}

func loadSiteMode(domain string) string {
	data, err := os.ReadFile(siteModePath(domain))
	if err != nil {
		return "markdown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "mode=html" {
			return "html"
		}
	}
	return "markdown"
}

func saveSiteMode(domain, mode string) error {
	if mode != "html" {
		mode = "markdown"
	}
	return os.WriteFile(siteModePath(domain), []byte("mode="+mode+"\n"), 0664)
}

func saveUploadedHomeHTML(domain string, htmlData []byte) (string, error) {
	if len(htmlData) == 0 {
		return "", fmt.Errorf("empty HTML file")
	}
	if len(htmlData) > 1<<20 {
		return "", fmt.Errorf("HTML file must be 1 MB or smaller")
	}
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		return "", err
	}
	uploadPath := nextHomeHTMLBackupPath(domain)
	uploadName := filepath.Base(uploadPath)
	if err := writeFileAtomic(uploadPath, htmlData, 0664); err != nil {
		return "", err
	}
	if err := recordUploadedHTML(domain, uploadName); err != nil {
		return "", err
	}
	if err := publishHomeHTML(domain, htmlData); err != nil {
		return "", err
	}
	return uploadName, nil
}

func saveUploadedSiteHTML(domain, originalName string, htmlData []byte) (string, bool, error) {
	if len(htmlData) == 0 {
		return "", false, fmt.Errorf("empty HTML file")
	}
	if len(htmlData) > 1<<20 {
		return "", false, fmt.Errorf("HTML file must be 1 MB or smaller")
	}

	name := cleanUploadedHTMLName(originalName)
	if name == "index.html" || name == "" {
		uploadName, err := saveUploadedHomeHTML(domain, htmlData)
		return uploadName, true, err
	}
	if !allowedUploadedHTMLName(name) || reservedSiteHTMLName(name) || isHomeHTMLName(name) {
		return "", false, fmt.Errorf("invalid HTML file name")
	}
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		return "", false, err
	}
	if err := writeFileAtomic(filepath.Join(domainSiteBase(domain), name), htmlData, 0664); err != nil {
		return "", false, err
	}
	if err := recordUploadedHTML(domain, name); err != nil {
		return "", false, err
	}
	return name, false, nil
}

func backupHomeHTML(domain string) (string, error) {
	if _, err := os.Stat(homeHTMLPath(domain)); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	backup := nextHomeHTMLBackupPath(domain)
	if err := os.Rename(homeHTMLPath(domain), backup); err != nil {
		return "", err
	}
	if isUploadedHTML(domain, "index.html") {
		if err := recordUploadedHTML(domain, filepath.Base(backup)); err != nil {
			return "", err
		}
	}
	return filepath.Base(backup), nil
}

func nextHomeHTMLBackupPath(domain string) string {
	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		backup := filepath.Join(domainSiteBase(domain), "index-"+now.Add(time.Duration(i)*time.Second).Format("20060102T150405Z")+".html")
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			return backup
		}
	}
	return filepath.Join(domainSiteBase(domain), "index-"+now.Format("20060102T150405Z")+".html")
}

func listSiteHTMLFiles(domain string) ([]SiteHTMLFile, error) {
	entries, err := os.ReadDir(domainSiteBase(domain))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	uploaded := loadUploadedHTMLNames(domain)

	var files []SiteHTMLFile
	for _, entry := range entries {
		if entry.IsDir() || !allowedUploadedHTMLName(entry.Name()) {
			continue
		}
		if entry.Name() != "index.html" && !isHomeHTMLName(entry.Name()) && !uploaded[entry.Name()] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, SiteHTMLFile{
			Name:        entry.Name(),
			Active:      entry.Name() == "index.html",
			Uploaded:    uploaded[entry.Name()],
			Homepage:    isHomeHTMLName(entry.Name()),
			Size:        info.Size(),
			DisplaySize: displaySize(info.Size()),
			Modified:    info.ModTime().Format("2006-01-02 15:04"),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].Active != files[j].Active {
			return files[i].Active
		}
		return files[i].Name > files[j].Name
	})

	return files, nil
}

func restoreSiteHTMLFile(domain, name string) error {
	if !isHomeHTMLName(name) {
		return fmt.Errorf("invalid HTML file")
	}
	if name == "index.html" {
		return saveSiteMode(domain, "html")
	}
	data, err := os.ReadFile(filepath.Join(domainSiteBase(domain), name))
	if err != nil {
		return err
	}
	return publishHomeHTML(domain, data)
}

func restoreLatestSiteHTMLBackup(domain string) (string, error) {
	files, err := listSiteHTMLFiles(domain)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if !file.Active && file.Homepage && file.Uploaded {
			return file.Name, restoreSiteHTMLFile(domain, file.Name)
		}
	}
	for _, file := range files {
		if !file.Active && file.Homepage {
			return file.Name, restoreSiteHTMLFile(domain, file.Name)
		}
	}
	return "", fmt.Errorf("no HTML backup found")
}

func deleteSiteHTMLFile(domain, name string) error {
	if !allowedUploadedHTMLName(name) || name == "index.html" || reservedSiteHTMLName(name) {
		return fmt.Errorf("invalid HTML file")
	}
	if err := os.Remove(filepath.Join(domainSiteBase(domain), name)); err != nil {
		return err
	}
	return removeUploadedHTML(domain, name)
}

func loadUploadedHTMLNames(domain string) map[string]bool {
	names := map[string]bool{}
	data, err := os.ReadFile(htmlUploadsPath(domain))
	if err != nil {
		return names
	}
	for _, line := range strings.Split(string(data), "\n") {
		name := strings.TrimSpace(line)
		if allowedUploadedHTMLName(name) {
			names[name] = true
		}
	}
	return names
}

func isUploadedHTML(domain, name string) bool {
	return loadUploadedHTMLNames(domain)[name]
}

func recordUploadedHTML(domain, name string) error {
	if !allowedUploadedHTMLName(name) {
		return fmt.Errorf("invalid HTML file")
	}
	names := loadUploadedHTMLNames(domain)
	names[name] = true
	return saveUploadedHTMLNames(domain, names)
}

func removeUploadedHTML(domain, name string) error {
	names := loadUploadedHTMLNames(domain)
	delete(names, name)
	return saveUploadedHTMLNames(domain, names)
}

func saveUploadedHTMLNames(domain string, names map[string]bool) error {
	var kept []string
	for name := range names {
		if allowedUploadedHTMLName(name) {
			kept = append(kept, name)
		}
	}
	sort.Strings(kept)
	var b strings.Builder
	for _, name := range kept {
		fmt.Fprintln(&b, name)
	}
	return os.WriteFile(htmlUploadsPath(domain), []byte(b.String()), 0664)
}

func allowedUploadedHTMLName(name string) bool {
	raw := strings.TrimSpace(name)
	if raw != filepath.Base(raw) {
		return false
	}
	name = cleanUploadedHTMLName(raw)
	return uploadedHTMLNamePattern.MatchString(name)
}

func cleanUploadedHTMLName(name string) string {
	name = strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

func isHomeHTMLName(name string) bool {
	return homeHTMLNamePattern.MatchString(strings.TrimSpace(name))
}

func reservedSiteHTMLName(name string) bool {
	switch cleanUploadedHTMLName(name) {
	case "contact.html", "thankyou.html":
		return true
	default:
		return false
	}
}

func publishHomeHTML(domain string, htmlData []byte) error {
	if err := writeFileAtomic(homeHTMLPath(domain), htmlData, 0664); err != nil {
		return err
	}
	return saveSiteMode(domain, "html")
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0775); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func renderHomeHTML(markdown string) []byte {
	content := renderMarkdown(markdown)
	var b bytes.Buffer
	fmt.Fprint(&b, "<!DOCTYPE html>\n<html>\n<head>\n")
	fmt.Fprint(&b, `<meta charset="UTF-8">`+"\n")
	fmt.Fprint(&b, `<meta name="viewport" content="width=device-width, initial-scale=1.0">`+"\n")
	fmt.Fprint(&b, "<title>LocalBox</title>\n")
	fmt.Fprint(&b, "<style>\n")
	fmt.Fprint(&b, "body{font-family:sans-serif;margin:0;background:#f5f5f5;color:#1a1a2e;line-height:1.6;}\n")
	fmt.Fprint(&b, "main{max-width:760px;margin:48px auto;padding:0 22px;}\n")
	fmt.Fprint(&b, "article{background:white;border-radius:8px;padding:34px;}\n")
	fmt.Fprint(&b, "h1,h2,h3{line-height:1.2;}a{color:#00a0c0;}li{margin:6px 0;}img{max-width:100%;height:auto;border-radius:6px;}\n")
	fmt.Fprint(&b, "@media(max-width:700px){main{margin:24px auto;}article{padding:24px;}}\n")
	fmt.Fprint(&b, "</style>\n</head>\n<body>\n<main><article>\n")
	b.WriteString(content)
	fmt.Fprint(&b, "\n</article></main>\n</body>\n</html>\n")
	return b.Bytes()
}

func renderMarkdown(markdown string) string {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	var b strings.Builder
	inList := false
	var paragraph []string

	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		fmt.Fprintf(&b, "<p>%s</p>\n", renderInline(strings.Join(paragraph, " ")))
		paragraph = nil
	}
	closeList := func() {
		if !inList {
			return
		}
		b.WriteString("</ul>\n")
		inList = false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushParagraph()
			closeList()
			continue
		}

		if strings.HasPrefix(trimmed, "# ") {
			flushParagraph()
			closeList()
			fmt.Fprintf(&b, "<h1>%s</h1>\n", renderInline(strings.TrimSpace(trimmed[2:])))
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			flushParagraph()
			closeList()
			fmt.Fprintf(&b, "<h2>%s</h2>\n", renderInline(strings.TrimSpace(trimmed[3:])))
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			flushParagraph()
			closeList()
			fmt.Fprintf(&b, "<h3>%s</h3>\n", renderInline(strings.TrimSpace(trimmed[4:])))
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			flushParagraph()
			if !inList {
				b.WriteString("<ul>\n")
				inList = true
			}
			fmt.Fprintf(&b, "<li>%s</li>\n", renderInline(strings.TrimSpace(trimmed[2:])))
			continue
		}

		closeList()
		paragraph = append(paragraph, trimmed)
	}

	flushParagraph()
	closeList()
	return b.String()
}

func renderInline(text string) string {
	tokens := inlineTokens(text)
	if len(tokens) == 0 {
		return html.EscapeString(text)
	}

	var b strings.Builder
	pos := 0
	for _, token := range tokens {
		b.WriteString(html.EscapeString(text[pos:token.start]))
		b.WriteString(token.html)
		pos = token.end
	}
	b.WriteString(html.EscapeString(text[pos:]))
	return b.String()
}

func inlineTokens(text string) []inlineToken {
	var tokens []inlineToken

	for _, match := range markdownLinkedImagePattern.FindAllStringSubmatchIndex(text, -1) {
		alt := text[match[2]:match[3]]
		src := strings.TrimSpace(text[match[4]:match[5]])
		title := ""
		if match[6] >= 0 {
			title = text[match[6]:match[7]]
		}
		href := strings.TrimSpace(text[match[8]:match[9]])
		if !safeImage(src) || !safeLink(href) {
			continue
		}
		tokens = append(tokens, inlineToken{
			start: match[0],
			end:   match[1],
			html:  fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(href), renderImage(src, alt, title)),
		})
	}

	for _, match := range markdownImagePattern.FindAllStringSubmatchIndex(text, -1) {
		alt := text[match[2]:match[3]]
		src := strings.TrimSpace(text[match[4]:match[5]])
		title := ""
		if match[6] >= 0 {
			title = text[match[6]:match[7]]
		}
		if !safeImage(src) {
			continue
		}
		tokens = append(tokens, inlineToken{
			start: match[0],
			end:   match[1],
			html:  renderImage(src, alt, title),
		})
	}

	for _, match := range markdownLinkPattern.FindAllStringSubmatchIndex(text, -1) {
		label := text[match[2]:match[3]]
		href := strings.TrimSpace(text[match[4]:match[5]])
		if !safeLink(href) {
			continue
		}
		tokens = append(tokens, inlineToken{
			start: match[0],
			end:   match[1],
			html:  fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(href), html.EscapeString(label)),
		})
	}

	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].start == tokens[j].start {
			return tokens[i].end > tokens[j].end
		}
		return tokens[i].start < tokens[j].start
	})

	filtered := tokens[:0]
	end := 0
	for _, token := range tokens {
		if token.start < end {
			continue
		}
		filtered = append(filtered, token)
		end = token.end
	}

	return filtered
}

func renderImage(src, alt, title string) string {
	if strings.TrimSpace(title) == "" {
		return fmt.Sprintf(
			`<img src="%s" alt="%s" loading="lazy">`,
			html.EscapeString(src),
			html.EscapeString(alt),
		)
	}
	return fmt.Sprintf(
		`<img src="%s" alt="%s" title="%s" loading="lazy">`,
		html.EscapeString(src),
		html.EscapeString(alt),
		html.EscapeString(title),
	)
}

func safeLink(href string) bool {
	return strings.HasPrefix(href, "/") ||
		strings.HasPrefix(href, "https://") ||
		strings.HasPrefix(href, "http://") ||
		strings.HasPrefix(href, "mailto:")
}

func safeImage(src string) bool {
	return strings.HasPrefix(src, "/") ||
		strings.HasPrefix(src, "https://") ||
		strings.HasPrefix(src, "http://")
}
