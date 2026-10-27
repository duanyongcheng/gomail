package gmail

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/bary/pebble-go/internal/googleapi"
)

// RemoteWrite is the provider-neutral replay interface used by the durable
// pending-op worker. Payloads are persisted JSON, never ad-hoc SQL strings.
type RemoteWrite interface {
	Replay(context.Context, string, json.RawMessage) (outcomeUnknown bool, err error)
}

type writePayload struct {
	RemoteID     string   `json:"remote_id"`
	AddLabels    []string `json:"add_labels,omitempty"`
	RemoveLabels []string `json:"remove_labels,omitempty"`
}

func (p *Provider) Replay(ctx context.Context, opType string, raw json.RawMessage) (bool, error) {
	var payload writePayload
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
	if err := p.API.Modify(ctx, payload.RemoteID, payload.AddLabels, payload.RemoveLabels); err != nil {
		var remote *googleapi.HTTPError
		if errors.As(err, &remote) && remote.Status >= 400 && remote.Status < 500 && remote.Status != 401 && remote.Status != 403 && remote.Status != 429 {
			return false, errors.New("Gmail rejected this change")
		}
		return false, err
	}
	return false, nil
}

func LabelsForFlags(isRead, isStarred *bool) (add, remove []string) {
	if isRead != nil {
		if *isRead {
			remove = append(remove, "UNREAD")
		} else {
			add = append(add, "UNREAD")
		}
	}
	if isStarred != nil {
		if *isStarred {
			add = append(add, "STARRED")
		} else {
			remove = append(remove, "STARRED")
		}
	}
	return
}

func ArchiveLabels() (add, remove []string) { return nil, []string{"INBOX"} }
func RestoreLabels() (add, remove []string) { return []string{"INBOX"}, nil }
func TrashLabels() (add, remove []string)   { return []string{"TRASH"}, []string{"INBOX"} }
func MoveLabels(fromRemote, toRemote string) (add, remove []string) {
	if fromRemote != "" && !strings.EqualFold(fromRemote, toRemote) {
		remove = append(remove, fromRemote)
	}
	if toRemote != "" && !strings.EqualFold(fromRemote, toRemote) {
		add = append(add, toRemote)
	}
	return add, remove
}
