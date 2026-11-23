package googleapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/duanyongcheng/gomail/internal/domain"
)

// ValidateLabels prevents an incomplete/corrupt list from becoming a deletion
// snapshot. Gmail label listing is unpaginated and must include system INBOX.
func ValidateLabels(labels []Label) error {
	if len(labels) == 0 || len(labels) > 10000 {
		return errors.New("invalid Gmail label list")
	}
	seen := map[string]bool{}
	inbox := false
	for _, label := range labels {
		if label.ID == "" || len(label.ID) > 512 || strings.ContainsAny(label.ID, "/\\\x00\r\n") || label.Name == "" || !utf8.ValidString(label.Name) || (label.Type != "system" && label.Type != "user") || seen[label.ID] {
			return errors.New("invalid Gmail label list")
		}
		seen[label.ID] = true
		if label.ID == "INBOX" && label.Type == "system" {
			inbox = true
		}
	}
	if !inbox {
		return errors.New("incomplete Gmail label list")
	}
	return nil
}

type LabelWriteResult struct {
	Label     Label
	Uncertain bool
}

// ChangeLabel never retries a management write or follows redirects. The caller
// persists an intent before calling; uncertain outcomes must be reconciled by GET.
func (a *API) ChangeLabel(ctx context.Context, action, id, name string) (LabelWriteResult, error) {
	if action != "create" && action != "rename" && action != "delete" {
		return LabelWriteResult{}, errors.New("invalid label action")
	}
	if action != "create" && (id == "" || domain.GmailSystemLabel(id) || len(id) > 512 || strings.ContainsAny(id, "/\\\x00\r\n")) {
		return LabelWriteResult{}, errors.New("invalid custom label id")
	}
	if action != "delete" {
		var err error
		name, err = domain.ValidateGmailLabelName(name)
		if err != nil {
			return LabelWriteResult{}, err
		}
	}
	method, path := http.MethodPost, "/labels"
	var body any = struct {
		Name string `json:"name"`
	}{name}
	var output *Label = &Label{}
	if action == "rename" {
		method, path = http.MethodPatch, "/labels/"+url.PathEscape(id)
	}
	if action == "delete" {
		method, path, body, output = http.MethodDelete, "/labels/"+url.PathEscape(id), nil, nil
	}
	client := *a.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	session := *a
	session.HTTP = &client
	var target any
	if output != nil {
		target = output
	}
	err := session.request(ctx, method, path, nil, body, target)
	if err != nil {
		var status *HTTPError
		uncertain := true
		if errors.As(err, &status) && status.Status >= 400 && status.Status < 500 && status.Status != 408 {
			uncertain = false
		}
		if action == "delete" && errors.As(err, &status) && status.Status == 404 {
			return LabelWriteResult{}, nil
		}
		return LabelWriteResult{Uncertain: uncertain}, errors.New("Gmail label change failed; refresh labels before retrying")
	}
	if output != nil {
		if err := ValidateLabels([]Label{{ID: "INBOX", Name: "INBOX", Type: "system"}, *output}); err != nil {
			return LabelWriteResult{Uncertain: true}, errors.New("Gmail label response was incomplete; refresh labels")
		}
		if output.ID == "" || output.Name != name || output.Type != "user" || domain.GmailSystemLabel(output.ID) || (action == "rename" && output.ID != id) {
			return LabelWriteResult{Uncertain: true}, errors.New("Gmail label response was incomplete; refresh labels")
		}
		return LabelWriteResult{Label: *output}, nil
	}
	return LabelWriteResult{}, nil
}
