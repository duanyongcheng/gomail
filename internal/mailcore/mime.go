package mailcore

import "github.com/duanyongcheng/gomail/internal/mailmime"

// The loopback protocol prototype shares the same MIME parser as providers.
var Parse = mailmime.Parse
var Build = mailmime.Build
