package store

import "strings"

// buildSnippet preserves the v13 data migration snippet semantics. This extracts
// previews only; it is not the HTML renderer or a security sanitizer.
func buildSnippet(bodyText, bodyHTML string) string {
	value := strings.TrimSpace(stripHTMLForSnippet(bodyText))
	if value == "" && bodyHTML != "" {
		value = stripHTMLForSnippet(bodyHTML)
	}
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 200 {
		return string(runes[:200]) + "..."
	}
	return value
}

func stripHTMLForSnippet(value string) string {
	for _, tag := range []string{"style", "script"} {
		lower := asciiLower(value)
		var cleaned strings.Builder
		position := 0
		for {
			start := strings.Index(lower[position:], "<"+tag)
			if start == -1 {
				break
			}
			cleaned.WriteString(value[position : position+start])
			start += position
			end := strings.Index(lower[start:], "</"+tag)
			if end == -1 {
				position = len(value)
				break
			}
			end += start
			closeEnd := strings.IndexByte(lower[end:], '>')
			if closeEnd == -1 {
				position = end + len(tag) + 2
			} else {
				position = end + closeEnd + 1
			}
		}
		cleaned.WriteString(value[position:])
		value = cleaned.String()
	}
	var text strings.Builder
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
			text.WriteByte(' ')
		default:
			if !inTag {
				text.WriteRune(r)
			}
		}
	}
	// The legacy contract decodes these entities in this order, including
	// nested &amp;lt;. A general HTML decoder would change stored previews.
	result := text.String()
	for _, entity := range [][2]string{
		{"&amp;", "&"}, {"&lt;", "<"}, {"&gt;", ">"},
		{"&quot;", "\""}, {"&#39;", "'"}, {"&nbsp;", " "},
	} {
		result = strings.ReplaceAll(result, entity[0], entity[1])
	}
	return result
}

func asciiLower(value string) string {
	bytes := []byte(value)
	for i, b := range bytes {
		if b >= 'A' && b <= 'Z' {
			bytes[i] += 'a' - 'A'
		}
	}
	return string(bytes)
}
