package domain

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

func GmailSystemLabel(id string) bool {
	switch strings.ToUpper(id) {
	case "INBOX", "SENT", "DRAFT", "DRAFTS", "TRASH", "SPAM", "UNREAD", "STARRED", "IMPORTANT", "CHAT", "CHATS", "ALL", "ALL MAIL", "CATEGORY_PERSONAL", "CATEGORY_SOCIAL", "CATEGORY_PROMOTIONS", "CATEGORY_UPDATES", "CATEGORY_FORUMS":
		return true
	}
	return false
}
func ValidateGmailLabelName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || len(name) == 0 || len(name) > 225 {
		return "", errors.New("label name must be 1–225 UTF-8 bytes")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("label name contains control characters")
		}
	}
	if GmailSystemLabel(name) {
		return "", errors.New("system label names are reserved")
	}
	return name, nil
}
