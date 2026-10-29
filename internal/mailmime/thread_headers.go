package mailmime

import (
	"strings"

	"github.com/emersion/go-message/mail"
)

// ThreadHeaderIDs tolerates malformed optional threading headers without
// rejecting otherwise readable mail. Canonical IDs omit angle brackets.
func ThreadHeaderIDs(value string) []string {
	if len(value) > 16<<10 {
		return nil
	}
	var header mail.Header
	header.Set("References", value)
	ids, err := header.MsgIDList("References")
	if err != nil {
		ids = strings.Fields(value)
	}
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.Trim(id, "<>")
		if !validMessageID(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) > 100 {
		out = append(out[:1], out[len(out)-99:]...)
	}
	return out
}

func NormalizeSubject(value string) string {
	value = strings.TrimSpace(value)
	for {
		changed := false
		for _, prefix := range []string{"re:", "fwd:", "fw:", "回复:", "转发:"} {
			if len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix) {
				value = strings.TrimSpace(value[len(prefix):])
				changed = true
				break
			}
		}
		if !changed {
			return value
		}
	}
}

// ReplyReferences retains the root and closest ancestors when trimming long
// chains. The immediate parent's Message-ID is always the last entry.
func ReplyReferences(references, inReplyTo, parentID string) []string {
	ids := ThreadHeaderIDs(references)
	if len(ids) == 0 {
		ids = ThreadHeaderIDs(inReplyTo)
	}
	out := []string{}
	for _, id := range ids {
		if id != parentID {
			out = append(out, id)
		}
	}
	if validMessageID(parentID) {
		out = append(out, parentID)
	}
	if len(out) > 100 {
		out = append(out[:1], out[len(out)-99:]...)
	}
	return out
}
