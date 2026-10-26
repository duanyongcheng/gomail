package main

import (
	"github.com/bary/pebble-go/internal/mailcore"
	"github.com/emersion/go-message/mail"
)

// Only synthetic MIME fixture construction is reused from the IMAP slice.
func wireFixture(subject, id string) ([]byte, error) {
	html := `<style>.fixture-title {color:#d4714e;font-weight:bold} td {padding:8px}</style>
<h2 class="fixture-title">Gmail 中文邮件 / HTML compatibility</h2>
<p>OAuth → Gmail RAW MIME → SQLite → React。こんにちは 안녕하세요.</p>
<table border="1"><tr><td>Gmail API</td><td>中文搜索</td></tr></table>
<img src="cid:poc-inline" width="48" height="48" alt="inline fixture">
<script>window.__pocXss=1</script><img src="data:,invalid" onerror="window.__pocXss=2">`
	return mailcore.Build("sender@example.invalid", subject, "中文搜索 Gmail OAuth API Hello world", html, id, []*mail.Address{{Address: "demo@gmail.example.invalid"}}, nil, []mailcore.Attachment{{MIMEType: "image/png", ContentID: "poc-inline", Inline: true, Data: fixtureIcon()}})
}
