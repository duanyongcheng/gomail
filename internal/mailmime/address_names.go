package mailmime

import (
	"mime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/emersion/go-message"
)

// DecodeAddressName decodes residual RFC 2047 words in a display name. Some
// senders quote encoded words, which net/mail intentionally treats as literal
// quoted-string text. Call this after parsing addresses, never on the whole
// address header: decoding a name must not change mailbox boundaries.
// Cached metadata uses the same compatibility decoding without rewriting mail.
func DecodeAddressName(name string) string {
	if !strings.Contains(name, "=?") {
		return name
	}
	decoder := mime.WordDecoder{CharsetReader: message.CharsetReader}
	decoded, err := decoder.DecodeHeader(name)
	if err != nil || !utf8.ValidString(decoded) {
		return name
	}
	for _, r := range decoded {
		if unicode.IsControl(r) {
			return name
		}
	}
	return decoded
}
