package main

import (
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
	Content            string
	Preview            template.HTML
	Images             []SiteImage
	HTMLFiles          []SiteHTMLFile
	MenuPages          []SiteHTMLFile
	CatalogRows        []CatalogEditorRow
	DesignDrafts       []DesignDraft
	Domain             string
	Mode               string
	Section            string
	AssistantProvider  string
	AssistantBilling   string
	AssistantDraft     string
	AssistantDraftName string
	AssistantPreview   template.HTML
	AssistantError     string
	AssistantSuccess   string
	Error              string
	Success            string
	ImageError         string
	ImageSuccess       string
	UploadError        string
	UploadSuccess      string
	PastedHTML         string
	PastedHTMLName     string
}

type SiteHTMLFile struct {
	Name        string
	Active      bool
	Uploaded    bool
	Homepage    bool
	InMenu      bool
	MenuLabel   string
	Link        string
	HTMLLink    string
	Size        int64
	DisplaySize string
	Modified    string
}

type SiteMenuItem struct {
	Name  string
	Label string
}

type CatalogEditorRow struct {
	Index   int
	Product cartProduct
}

const shoppingPageName = "shopping.html"

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
	if err := os.WriteFile(homeHTMLPath(domain), renderHomeHTML(domain, markdown), 0664); err != nil {
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

func siteMenuPath(domain string) string {
	return filepath.Join(domainSiteBase(domain), "menu.conf")
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
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		return err
	}
	return os.WriteFile(siteModePath(domain), []byte("mode="+mode+"\n"), 0664)
}

func cleanCatalogProduct(product cartProduct) cartProduct {
	product.Name = cleanCatalogText(product.Name, 120)
	product.Description = cleanCatalogText(product.Description, 320)
	product.Price = cleanCatalogText(product.Price, 80)
	product.Category = cleanCatalogText(product.Category, 48)
	product.Image = cleanCatalogText(product.Image, 512)
	product.ID = cleanCatalogID(product.ID, product.Name)
	if product.Price == "" {
		product.Price = "Quoted"
	}
	if product.Category == "" {
		product.Category = "Core"
	}
	if strings.EqualFold(product.Category, "Add on") {
		product.Category = "Add-on"
	}
	return product
}

func cleanCatalogText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if len(text) > limit {
		text = strings.TrimSpace(text[:limit])
	}
	return text
}

func cleanCatalogID(id, fallback string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	var b strings.Builder
	lastDash := false
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	cleaned := strings.Trim(b.String(), "-")
	if cleaned == "" && strings.TrimSpace(fallback) != "" {
		return cleanCatalogID(fallback, "")
	}
	if cleaned == "" {
		return "catalog-item"
	}
	if len(cleaned) > 64 {
		cleaned = strings.Trim(cleaned[:64], "-")
	}
	return cleaned
}

func uniqueCatalogID(id string, seen map[string]bool) string {
	base := id
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !seen[candidate] {
			return candidate
		}
	}
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
	menu := loadSiteMenuMap(domain)

	var files []SiteHTMLFile
	for _, entry := range entries {
		if entry.IsDir() || !allowedUploadedHTMLName(entry.Name()) {
			continue
		}
		if entry.Name() != "index.html" && !isHomeHTMLName(entry.Name()) && !uploaded[entry.Name()] && !isGeneratedSiteHTMLName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		menuLabel, inMenu := menu[entry.Name()]
		files = append(files, SiteHTMLFile{
			Name:        entry.Name(),
			Active:      entry.Name() == "index.html",
			Uploaded:    uploaded[entry.Name()],
			Homepage:    isHomeHTMLName(entry.Name()),
			InMenu:      inMenu,
			MenuLabel:   menuLabel,
			Link:        siteHTMLFileLink(entry.Name()),
			HTMLLink:    siteHTMLFileHTMLLink(entry.Name()),
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

func siteHTMLFileLink(name string) string {
	if name == "index.html" {
		return "/"
	}
	return "/" + name
}

func siteHTMLFileHTMLLink(name string) string {
	label := defaultMenuLabel(name)
	return fmt.Sprintf(`<a href="%s">%s</a>`, siteHTMLFileLink(name), label)
}

func publishHTMLNavigation(domain string) error {
	data, err := os.ReadFile(homeHTMLPath(domain))
	if err != nil {
		return err
	}
	source := string(data)
	lower := strings.ToLower(source)
	closeNav := strings.Index(lower, "</nav>")
	if closeNav == -1 {
		return fmt.Errorf("active HTML homepage has no nav element")
	}

	var links []string
	for _, item := range existingSiteMenuItems(domain, loadSiteMenuItems(domain)) {
		href := siteHTMLFileLink(item.Name)
		if strings.Contains(source, `href="`+href+`"`) || strings.Contains(source, `href='`+href+`'`) {
			continue
		}
		links = append(links, fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(href), html.EscapeString(item.Label)))
	}
	if len(links) == 0 {
		return saveSiteMode(domain, "html")
	}

	insert := "\n" + strings.Join(links, "\n") + "\n"
	updated := source[:closeNav] + insert + source[closeNav:]
	if _, err := backupHomeHTML(domain); err != nil {
		return err
	}
	return publishHomeHTML(domain, []byte(updated))
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
	if err := removeSiteMenuItem(domain, name); err != nil {
		return err
	}
	return removeUploadedHTML(domain, name)
}

func siteMenuPages(files []SiteHTMLFile) []SiteHTMLFile {
	var pages []SiteHTMLFile
	for _, file := range files {
		if file.Active || file.Homepage {
			continue
		}
		if file.MenuLabel == "" {
			file.MenuLabel = defaultMenuLabel(file.Name)
		}
		pages = append(pages, file)
	}
	return pages
}

func loadSiteMenuMap(domain string) map[string]string {
	items := map[string]string{}
	for _, item := range loadSiteMenuItems(domain) {
		items[item.Name] = item.Label
	}
	return items
}

func loadSiteMenuItems(domain string) []SiteMenuItem {
	data, err := os.ReadFile(siteMenuPath(domain))
	if err != nil {
		return nil
	}

	var items []SiteMenuItem
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		name := cleanUploadedHTMLName(parts[0])
		if !allowedMenuHTMLName(name) || seen[name] {
			continue
		}
		label := ""
		if len(parts) == 2 {
			label = cleanMenuLabel(parts[1])
		}
		if label == "" {
			label = defaultMenuLabel(name)
		}
		items = append(items, SiteMenuItem{Name: name, Label: label})
		seen[name] = true
	}
	return items
}

func saveSiteMenuItems(domain string, items []SiteMenuItem) error {
	available := map[string]bool{}
	files, err := listSiteHTMLFiles(domain)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !file.Active && !file.Homepage {
			available[file.Name] = true
		}
	}

	seen := map[string]bool{}
	var b strings.Builder
	for _, item := range items {
		name := cleanUploadedHTMLName(item.Name)
		if !allowedMenuHTMLName(name) || !available[name] || seen[name] {
			continue
		}
		label := cleanMenuLabel(item.Label)
		if label == "" {
			label = defaultMenuLabel(name)
		}
		fmt.Fprintf(&b, "%s\t%s\n", name, label)
		seen[name] = true
	}
	return os.WriteFile(siteMenuPath(domain), []byte(b.String()), 0664)
}

func removeSiteMenuItem(domain, name string) error {
	name = cleanUploadedHTMLName(name)
	var kept []SiteMenuItem
	for _, item := range loadSiteMenuItems(domain) {
		if item.Name != name {
			kept = append(kept, item)
		}
	}
	return saveSiteMenuItems(domain, kept)
}

func existingSiteMenuItems(domain string, items []SiteMenuItem) []SiteMenuItem {
	var existing []SiteMenuItem
	for _, item := range items {
		if _, err := os.Stat(filepath.Join(domainSiteBase(domain), item.Name)); err == nil {
			existing = append(existing, item)
		}
	}
	return existing
}

func allowedMenuHTMLName(name string) bool {
	return allowedUploadedHTMLName(name) && !isHomeHTMLName(name) && !reservedSiteHTMLName(name)
}

func cleanMenuLabel(label string) string {
	label = strings.Join(strings.Fields(strings.TrimSpace(label)), " ")
	if len(label) > 48 {
		label = strings.TrimSpace(label[:48])
	}
	return label
}

func defaultMenuLabel(name string) string {
	name = strings.TrimSuffix(cleanUploadedHTMLName(name), ".html")
	words := strings.Fields(strings.ReplaceAll(name, "-", " "))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func isGeneratedSiteHTMLName(name string) bool {
	return cleanUploadedHTMLName(name) == shoppingPageName
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

func saveShoppingCartPage(domain string) error {
	products, addOns := loadSiteCatalog(domain)
	menu := loadSiteMenuItems(domain)
	htmlData := []byte(newShoppingCartComponentManager(domain, "", "", menu, products, addOns).Render())
	if err := writeFileAtomic(filepath.Join(domainSiteBase(domain), shoppingPageName), htmlData, 0664); err != nil {
		return err
	}
	if err := recordUploadedHTML(domain, shoppingPageName); err != nil {
		return err
	}
	if err := ensureSiteMenuItem(domain, SiteMenuItem{Name: shoppingPageName, Label: "Shopping"}); err != nil {
		return err
	}
	return nil
}

func ensureSiteMenuItem(domain string, item SiteMenuItem) error {
	items := loadSiteMenuItems(domain)
	for i, existing := range items {
		if existing.Name == item.Name {
			items[i].Label = item.Label
			return saveSiteMenuItems(domain, items)
		}
	}
	items = append(items, item)
	return saveSiteMenuItems(domain, items)
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

func renderHomeHTML(domain, markdown string) []byte {
	content := renderMarkdown(markdown)
	menu := existingSiteMenuItems(domain, loadSiteMenuItems(domain))
	return []byte(newContentPageComponentManager(content, menu).Render())
}

func renderContentPageHTML(content string, menu []SiteMenuItem) string {
	return newContentPageComponentManager(content, menu).Render()
}

func renderShoppingCartHTML(domain string, menu []SiteMenuItem) string {
	products, addOns := loadSiteCatalog(domain)
	return newShoppingCartComponentManager(domain, "", "", menu, products, addOns).Render()
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
