// Package privacy renders untrusted message HTML for the isolated mail reader.
// It is deliberately independent of MyGo and never serves local files itself.
package privacy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

type Mode string

const (
	Strict   Mode = "Strict"
	LoadOnce Mode = "LoadOnce"
	Off      Mode = "Off"
)

type TrustedSender struct{ Email string }

type Attachment struct {
	ID        string
	ContentID string
}

type Tracker struct {
	Domain string `json:"domain"`
	Type   string `json:"tracker_type"`
}

type Rendered struct {
	HTML            string
	TrackersBlocked []Tracker
	ImagesBlocked   int
}

func ParseMode(raw json.RawMessage) Mode {
	if len(raw) == 0 || string(raw) == "null" {
		return Strict
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		switch Mode(text) {
		case Strict, LoadOnce, Off:
			return Mode(text)
		}
	}
	var trusted map[string]string
	if json.Unmarshal(raw, &trusted) == nil && trusted["TrustedSender"] != "" {
		return Mode("trusted:" + trusted["TrustedSender"])
	}
	return Strict
}

func (m Mode) trusted() bool                   { return strings.HasPrefix(string(m), "trusted:") }
func (m Mode) allowsRemoteImages() bool        { return m == LoadOnce || m == Off || m.trusted() }
func (m Mode) allowsStyles() bool              { return m == LoadOnce || m == Off || m.trusted() }
func (m Mode) allowsExternalStylesheets() bool { return m == Off }

func AttachmentURL(accountID, messageID, attachmentID string) string {
	token := base64.RawURLEncoding.EncodeToString([]byte(accountID + "\x00" + messageID + "\x00" + attachmentID))
	return "pebble-attachment://localhost/" + token
}

func DecodeAttachmentURL(path string) (accountID, messageID, attachmentID string, ok bool) {
	token := strings.Trim(path, "/")
	if token == "" {
		return "", "", "", false
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", "", false
	}
	parts := strings.Split(string(data), "\x00")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func Render(rawHTML, bodyText, accountID, messageID string, attachments []Attachment, mode Mode) Rendered {
	if strings.TrimSpace(rawHTML) == "" {
		return Rendered{HTML: "<pre class=\"pebble-plain-text-email\">" + html.EscapeString(bodyText) + "</pre>"}
	}
	root, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return Rendered{HTML: "<pre class=\"pebble-plain-text-email\">" + html.EscapeString(bodyText) + "</pre>"}
	}
	ctx := renderer{mode: mode, accountID: accountID, messageID: messageID, cid: map[string]string{}, trackerKeys: map[string]bool{}}
	for _, attachment := range attachments {
		if attachment.ContentID != "" {
			ctx.cid[strings.ToLower(strings.Trim(attachment.ContentID, "<>"))] = AttachmentURL(accountID, messageID, attachment.ID)
		}
	}
	body := findBody(root)
	head := findHead(root)
	var b strings.Builder
	if head != nil {
		for child := head.FirstChild; child != nil; child = child.NextSibling {
			ctx.writeNode(&b, child)
		}
	}
	if body != nil {
		for child := body.FirstChild; child != nil; child = child.NextSibling {
			ctx.writeNode(&b, child)
		}
	} else {
		for child := root.FirstChild; child != nil; child = child.NextSibling {
			ctx.writeNode(&b, child)
		}
	}
	return Rendered{HTML: b.String(), TrackersBlocked: ctx.trackers, ImagesBlocked: ctx.imagesBlocked}
}

type renderer struct {
	mode                 Mode
	accountID, messageID string
	cid                  map[string]string
	trackers             []Tracker
	trackerKeys          map[string]bool
	imagesBlocked        int
}

var knownTrackers = []string{"mailchimp.com", "list-manage.com", "hubspot.com", "sendgrid.net", "mailgun.org", "constantcontact.com", "campaign-archive.com", "exacttarget.com", "sailthru.com", "returnpath.net", "litmus.com", "bananatag.com", "yesware.com", "mailtrack.io", "getnotify.com", "streak.com", "cirrusinsight.com", "boomeranggmail.com", "mixmax.com", "superhuman.com", "google-analytics.com", "doubleclick.net", "pixel.wp.com", "open.convertkit.com", "mandrillapp.com"}

var allowedTags = map[string]bool{"a": true, "abbr": true, "address": true, "article": true, "b": true, "bdi": true, "bdo": true, "blockquote": true, "br": true, "caption": true, "center": true, "cite": true, "code": true, "col": true, "colgroup": true, "dd": true, "del": true, "details": true, "dfn": true, "div": true, "dl": true, "dt": true, "em": true, "figcaption": true, "figure": true, "font": true, "footer": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hr": true, "i": true, "img": true, "ins": true, "kbd": true, "li": true, "main": true, "mark": true, "nav": true, "ol": true, "p": true, "pre": true, "q": true, "rp": true, "rt": true, "ruby": true, "s": true, "samp": true, "section": true, "small": true, "span": true, "strong": true, "sub": true, "summary": true, "sup": true, "table": true, "tbody": true, "td": true, "tfoot": true, "th": true, "thead": true, "time": true, "tr": true, "u": true, "ul": true, "var": true, "wbr": true}
var dropTags = map[string]bool{"script": true, "iframe": true, "object": true, "embed": true, "form": true, "input": true, "textarea": true, "button": true, "select": true, "meta": true, "base": true, "template": true, "svg": true, "canvas": true}
var allowedAttrs = map[string]bool{"class": true, "id": true, "dir": true, "lang": true, "title": true, "alt": true, "width": true, "height": true, "colspan": true, "rowspan": true, "border": true, "cellpadding": true, "cellspacing": true, "align": true, "valign": true, "bgcolor": true, "color": true, "face": true, "size": true, "style": true}

func findHead(root *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "head") {
			found = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}
func findBody(root *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "body") {
			found = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

func (r *renderer) writeNode(out *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		out.WriteString(html.EscapeString(n.Data))
		return
	case html.CommentNode:
		return
	case html.DoctypeNode:
		return
	case html.ElementNode:
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			r.writeNode(out, c)
		}
		return
	}
	tag := strings.ToLower(n.Data)
	if dropTags[tag] {
		return
	}
	if tag == "head" || tag == "title" {
		return
	}
	if tag == "style" {
		if r.mode.allowsStyles() {
			css := sanitizeCSS(nodeText(n))
			if css != "" {
				out.WriteString("<style>")
				out.WriteString(css)
				out.WriteString("</style>")
			}
		}
		return
	}
	if tag == "link" {
		if r.mode.allowsExternalStylesheets() && safeStylesheet(n) {
			out.WriteString("<link rel=\"stylesheet\" href=\"")
			out.WriteString(html.EscapeString(attr(n, "href")))
			out.WriteString("\">")
		}
		return
	}
	if tag == "img" {
		r.writeImage(out, n)
		return
	}
	if !allowedTags[tag] {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			r.writeNode(out, c)
		}
		return
	}
	out.WriteByte('<')
	out.WriteString(tag)
	for _, a := range n.Attr {
		name := strings.ToLower(a.Key)
		if strings.HasPrefix(name, "on") || name == "src" || name == "href" || name == "srcset" || name == "background" || name == "poster" || !allowedAttrs[name] {
			continue
		}
		value := a.Val
		if name == "style" {
			value = sanitizeCSS(value)
		}
		if value == "" && name == "style" {
			continue
		}
		out.WriteByte(' ')
		out.WriteString(name)
		out.WriteString("=\"")
		out.WriteString(html.EscapeString(value))
		out.WriteString("\"")
	}
	if tag == "a" {
		href := strings.TrimSpace(attr(n, "href"))
		if safeLink(href) {
			out.WriteString(" href=\"")
			out.WriteString(html.EscapeString(href))
			out.WriteString("\" target=\"_blank\" rel=\"noopener noreferrer\"")
		}
	}
	out.WriteByte('>')
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.writeNode(out, c)
	}
	if !voidTag(tag) {
		out.WriteString("</")
		out.WriteString(tag)
		out.WriteByte('>')
	}
}

func (r *renderer) writeImage(out *strings.Builder, n *html.Node) {
	src := strings.TrimSpace(attr(n, "src"))
	alt := attr(n, "alt")
	width, height := attr(n, "width"), attr(n, "height")
	if strings.HasPrefix(strings.ToLower(src), "cid:") {
		if target, ok := r.cid[strings.ToLower(strings.Trim(strings.TrimPrefix(src, "cid:"), "<>"))]; ok {
			out.WriteString("<img src=\"")
			out.WriteString(target)
			out.WriteString("\"")
			r.writeImageAttrs(out, n)
			out.WriteString(">")
		}
		return
	}
	if !safeRemoteImage(src) {
		return
	}
	domain := imageDomain(src)
	tracker := isPixel(width, height) || knownTracker(domain)
	if tracker && !r.mode.trusted() && r.mode != Off {
		r.addTracker(domain, map[bool]string{true: "pixel", false: "domain"}[isPixel(width, height)])
		return
	}
	if !r.mode.allowsRemoteImages() {
		r.imagesBlocked++
		out.WriteString("<div class=\"blocked-image\">")
		out.WriteString(html.EscapeString(alt))
		if strings.TrimSpace(alt) == "" {
			out.WriteString("Image blocked for privacy")
		}
		out.WriteString("</div>")
		return
	}
	out.WriteString("<img src=\"")
	out.WriteString(html.EscapeString(src))
	out.WriteString("\"")
	r.writeImageAttrs(out, n)
	out.WriteString(">")
}
func (r *renderer) writeImageAttrs(out *strings.Builder, n *html.Node) {
	for _, name := range []string{"alt", "width", "height", "class", "title"} {
		if v := attr(n, name); v != "" {
			out.WriteByte(' ')
			out.WriteString(name)
			out.WriteString("=\"")
			out.WriteString(html.EscapeString(v))
			out.WriteString("\"")
		}
	}
}
func (r *renderer) addTracker(domain, typ string) {
	key := domain + "\x00" + typ
	if r.trackerKeys == nil {
		r.trackerKeys = map[string]bool{}
	}
	if r.trackerKeys[key] {
		return
	}
	r.trackerKeys[key] = true
	r.trackers = append(r.trackers, Tracker{Domain: domain, Type: typ})
	sort.Slice(r.trackers, func(i, j int) bool { return r.trackers[i].Domain < r.trackers[j].Domain })
}
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}
func nodeText(n *html.Node) string {
	var b strings.Builder
	var w func(*html.Node)
	w = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			w(c)
		}
	}
	w(n)
	return b.String()
}
func voidTag(tag string) bool {
	switch tag {
	case "br", "hr", "img", "col", "wbr":
		return true
	}
	return false
}
func safeLink(v string) bool {
	u, err := url.Parse(v)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto"
}
func safeStylesheet(n *html.Node) bool {
	return strings.EqualFold(attr(n, "rel"), "stylesheet") && safeRemoteImage(attr(n, "href"))
}
func safeRemoteImage(v string) bool {
	u, err := url.Parse(v)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}
func imageDomain(v string) string {
	u, err := url.Parse(v)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
func isPixel(w, h string) bool {
	if w == "" && h == "" {
		return false
	}
	parse := func(v string) int { var n int; fmt.Sscanf(strings.TrimSpace(v), "%d", &n); return n }
	return (w != "" && parse(w) <= 1) || (h != "" && parse(h) <= 1)
}
func knownTracker(domain string) bool {
	for _, known := range knownTrackers {
		if domain == known || strings.HasSuffix(domain, "."+known) {
			return true
		}
	}
	return false
}

var cssProperty = regexp.MustCompile(`(?i)^[a-z-]+$`)

func sanitizeCSS(raw string) string {
	if strings.Contains(raw, "{") {
		var rules []string
		remaining := raw
		for {
			open := strings.IndexByte(remaining, '{')
			if open < 0 {
				break
			}
			close := strings.IndexByte(remaining[open+1:], '}')
			if close < 0 {
				break
			}
			close += open + 1
			selector := strings.TrimSpace(remaining[:open])
			if selector != "" && !strings.ContainsAny(selector, "@\\") {
				if declarations := sanitizeCSS(remaining[open+1 : close]); declarations != "" {
					rules = append(rules, selector+"{"+declarations+"}")
				}
			}
			remaining = remaining[close+1:]
		}
		return strings.Join(rules, "")
	}
	var out []string
	for _, part := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if !cssProperty.MatchString(name) || value == "" || !safeCSSValue(value) {
			continue
		}
		switch name {
		case "color", "background", "background-color", "font", "font-family", "font-size", "font-style", "font-weight", "line-height", "text-align", "text-decoration", "vertical-align", "display", "margin", "padding", "width", "height", "max-width", "max-height", "border", "border-color", "border-radius", "border-width", "border-style", "overflow", "opacity", "visibility", "white-space", "word-break", "table-layout":
			out = append(out, name+":"+value)
		}
	}
	return strings.Join(out, "; ")
}
func safeCSSValue(v string) bool {
	l := strings.ToLower(v)
	for _, bad := range []string{"url(", "image-set(", "expression(", "javascript:", "vbscript:", "data:", "@import", "\\"} {
		if strings.Contains(l, bad) {
			return false
		}
	}
	return true
}
