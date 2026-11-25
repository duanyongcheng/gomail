package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	DefaultMaxSummaryRunes       = 24000
	DefaultMaxThreadSummaryRunes = 32000
	MaxThreadSummaryMessages     = 20
	longURLThreshold             = 120
)

var (
	urlPattern        = regexp.MustCompile(`https?://[^\s<>"'\)\]]+`)
	multiBlankPattern = regexp.MustCompile(`\n{3,}`)
	multiSpacePattern = regexp.MustCompile(`[ \t]{2,}`)
)

type ExtractedEmail struct {
	Text        string
	Truncated   bool
	ContentHash string
}

type ThreadMessageInput struct {
	ID       string
	From     string
	Date     int64
	Subject  string
	BodyText string
	BodyHTML string
}

func HashContent(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ExtractMessageContent(subject, fromName, fromAddr, bodyText, bodyHTML string, maxRunes int) ExtractedEmail {
	if maxRunes <= 0 {
		maxRunes = DefaultMaxSummaryRunes
	}
	body := cleanBody(bodyText, bodyHTML)
	var header strings.Builder
	if s := strings.TrimSpace(subject); s != "" {
		header.WriteString("Subject: ")
		header.WriteString(s)
		header.WriteByte('\n')
	}
	sender := strings.TrimSpace(fromName)
	if addr := strings.TrimSpace(fromAddr); addr != "" {
		if sender != "" && !strings.EqualFold(sender, addr) {
			sender = sender + " <" + addr + ">"
		} else {
			sender = addr
		}
	}
	if sender != "" {
		header.WriteString("From: ")
		header.WriteString(sender)
		header.WriteByte('\n')
	}
	full := strings.TrimSpace(header.String() + "\n" + body)
	truncatedText, truncated := truncateBudget(full, maxRunes)
	return ExtractedEmail{
		Text:        truncatedText,
		Truncated:   truncated,
		ContentHash: HashContent(PromptVersion, "message", full),
	}
}

func ExtractThreadContent(messages []ThreadMessageInput, maxRunes int) ExtractedEmail {
	if maxRunes <= 0 {
		maxRunes = DefaultMaxThreadSummaryRunes
	}
	if len(messages) == 0 {
		return ExtractedEmail{}
	}
	truncated := false
	selected := messages
	if len(selected) > MaxThreadSummaryMessages {
		selected = selected[len(selected)-MaxThreadSummaryMessages:]
		truncated = true
	}
	hashParts := make([]string, 0, len(selected)*2+2)
	hashParts = append(hashParts, PromptVersion, "thread")

	perMsgBudget := maxRunes / len(selected)
	if perMsgBudget < 800 {
		perMsgBudget = 800
	}
	var sections []string
	for i, m := range selected {
		body := cleanBody(m.BodyText, m.BodyHTML)
		if i > 0 {
			// On follow-up messages, strip trailing quoted reply noise to save tokens.
			body = stripQuotedTail(body)
		}
		hashParts = append(hashParts, m.ID, body)
		bodyPart, cut := truncateBudget(body, perMsgBudget)
		if cut {
			truncated = true
		}
		ts := ""
		if m.Date > 0 {
			ts = time.Unix(m.Date, 0).UTC().Format("2006-01-02 15:04 UTC")
		}
		var sec strings.Builder
		sec.WriteString(fmt.Sprintf("--- Message %d ---\n", i+1))
		if strings.TrimSpace(m.From) != "" {
			sec.WriteString("From: " + strings.TrimSpace(m.From) + "\n")
		}
		if ts != "" {
			sec.WriteString("Date: " + ts + "\n")
		}
		if i == 0 && strings.TrimSpace(m.Subject) != "" {
			sec.WriteString("Subject: " + strings.TrimSpace(m.Subject) + "\n")
		}
		sec.WriteByte('\n')
		sec.WriteString(bodyPart)
		sections = append(sections, sec.String())
	}
	combined := strings.Join(sections, "\n\n")
	finalText, cut := truncateBudget(combined, maxRunes)
	if cut {
		truncated = true
	}
	return ExtractedEmail{
		Text:        finalText,
		Truncated:   truncated,
		ContentHash: HashContent(hashParts...),
	}
}

func cleanBody(bodyText, bodyHTML string) string {
	htmlText := ""
	if strings.TrimSpace(bodyHTML) != "" {
		htmlText = htmlToCleanText(bodyHTML)
	}
	plainText := normalizePlainText(bodyText)
	chosen := plainText
	if utf8.RuneCountInString(htmlText) > 0 && (utf8.RuneCountInString(plainText) == 0 || utf8.RuneCountInString(htmlText) > utf8.RuneCountInString(plainText)/2) {
		chosen = htmlText
	}
	return collapseLongURLs(chosen)
}

func htmlToCleanText(raw string) string {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return normalizePlainText(raw)
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			switch tag {
			case "script", "style", "head", "noscript", "svg", "template", "iframe", "object":
				return
			}
			if isHiddenElement(n) {
				return
			}
			switch tag {
			case "br":
				b.WriteByte('\n')
				return
			case "hr":
				b.WriteString("\n---\n")
				return
			case "p", "div", "section", "article", "header", "footer", "blockquote", "tr", "table":
				b.WriteByte('\n')
			case "h1", "h2", "h3", "h4", "h5", "h6":
				b.WriteString("\n# ")
			case "li":
				b.WriteString("\n- ")
			case "td", "th":
				b.WriteByte(' ')
			}
		} else if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if b.Len() > 0 {
					last := b.String()[b.Len()-1]
					if last != '\n' && last != ' ' && last != '\t' {
						b.WriteByte(' ')
					}
				}
				b.WriteString(text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "p", "div", "section", "article", "blockquote", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "li":
				b.WriteByte('\n')
			}
		}
	}
	walk(doc)
	return normalizePlainText(b.String())
}

func isHiddenElement(n *html.Node) bool {
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if key == "hidden" {
			return true
		}
		if key == "aria-hidden" && strings.EqualFold(strings.TrimSpace(a.Val), "true") {
			return true
		}
		if key == "style" {
			compact := strings.ToLower(strings.ReplaceAll(a.Val, " ", ""))
			if strings.Contains(compact, "display:none") ||
				strings.Contains(compact, "visibility:hidden") ||
				strings.Contains(compact, "opacity:0;") ||
				strings.HasSuffix(compact, "opacity:0") ||
				strings.Contains(compact, "font-size:0") ||
				strings.Contains(compact, "max-height:0") {
				return true
			}
		}
	}
	return false
}

func normalizePlainText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "\u200b", "")
	s = strings.ReplaceAll(s, "\ufeff", "")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(multiSpacePattern.ReplaceAllString(line, " "))
	}
	joined := strings.Join(lines, "\n")
	joined = multiBlankPattern.ReplaceAllString(joined, "\n\n")
	return strings.TrimSpace(joined)
}

func collapseLongURLs(s string) string {
	return urlPattern.ReplaceAllStringFunc(s, func(raw string) string {
		if len(raw) <= longURLThreshold {
			return raw
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "[link]"
		}
		return "[link: " + u.Host + "]"
	})
}

func stripQuotedTail(body string) string {
	lines := strings.Split(body, "\n")
	cutAt := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i > 1 && (strings.HasPrefix(trimmed, "-----Original Message-----") ||
			strings.HasPrefix(trimmed, "---------- Forwarded message") ||
			(strings.HasPrefix(trimmed, "On ") && strings.HasSuffix(trimmed, "wrote:")) ||
			(strings.HasPrefix(trimmed, "在 ") && strings.HasSuffix(trimmed, "写道："))) {
			cutAt = i
			break
		}
	}
	kept := make([]string, 0, cutAt)
	quoteRun := 0
	for i := 0; i < cutAt; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
			quoteRun++
			if quoteRun > 3 {
				continue
			}
		} else {
			quoteRun = 0
		}
		kept = append(kept, lines[i])
	}
	res := strings.TrimSpace(strings.Join(kept, "\n"))
	if res == "" {
		return body
	}
	return res
}

func truncateBudget(s string, maxRunes int) (string, bool) {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s, false
	}
	headLen := int(float64(maxRunes) * 0.8)
	tailLen := maxRunes - headLen
	if tailLen < 200 {
		tailLen = 0
		headLen = maxRunes
	}
	if tailLen == 0 {
		return string(runes[:headLen]) + "\n[...content truncated...]", true
	}
	return string(runes[:headLen]) + "\n\n[...middle content truncated...]\n\n" + string(runes[len(runes)-tailLen:]), true
}
