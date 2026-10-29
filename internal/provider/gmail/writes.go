package gmail

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/bary/pebble-go/internal/googleapi"
	"github.com/bary/pebble-go/internal/store"
)

// RemoteWrite is the provider-neutral replay interface used by the durable
// pending-op worker. Payloads are persisted JSON, never ad-hoc SQL strings.
type RemoteWrite interface {
	Replay(context.Context, string, json.RawMessage) (outcomeUnknown bool, err error)
}

func (p *Provider) Replay(ctx context.Context, opType string, raw json.RawMessage) (bool, error) {
	var payload store.MailOpPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false, errors.New("invalid pending operation payload")
	}
	if payload.RemoteID == "" {
		return false, errors.New("pending operation has no remote id")
	}
	switch opType {
	case "update_flags", "archive", "unarchive", "restore", "delete", "move_to_folder":
	default:
		return false, errors.New("unsupported pending operation")
	}
	p.gate.Lock()
	defer p.gate.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := p.DB.ValidateGmailLabelTargets(ctx, p.AccountID, payload); err != nil {
		return false, err
	}
	if err := p.API.Modify(ctx, payload.RemoteID, payload.AddLabels, payload.RemoveLabels); err != nil {
		var remote *googleapi.HTTPError
		if errors.As(err, &remote) && remote.Status >= 400 && remote.Status < 500 && remote.Status != 401 && remote.Status != 403 && remote.Status != 429 {
			return false, errors.New("Gmail rejected this change")
		}
		return false, err
	}
	return false, nil
}
