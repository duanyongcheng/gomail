package mailmime

type Address struct {
	Name    *string `json:"name"`
	Address string  `json:"address"`
}
type Attachment struct {
	Filename  string `json:"filename"`
	MIMEType  string `json:"mime_type"`
	ContentID string `json:"content_id"`
	Inline    bool   `json:"is_inline"`
	Data      []byte `json:"-"`
}
type Message struct {
	ID          string       `json:"id"`
	AccountID   string       `json:"account_id"`
	UID         uint32       `json:"uid"`
	UIDValidity uint32       `json:"uid_validity"`
	Subject     string       `json:"subject"`
	Snippet     string       `json:"snippet"`
	From        Address      `json:"from"`
	To          []Address    `json:"to_list"`
	BodyText    string       `json:"body_text"`
	BodyHTML    string       `json:"body_html_raw"`
	Date        int64        `json:"date"`
	IsRead      bool         `json:"is_read"`
	IsStarred   bool         `json:"is_starred"`
	MessageID   string       `json:"message_id_header"`
	Attachments []Attachment `json:"attachments"`
}

const MaxMessageBytes = 2 << 20
