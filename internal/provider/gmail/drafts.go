package gmail

import (
	"context"
	"github.com/bary/pebble-go/internal/googleapi"
)

// WithDraftSession serializes draft changes with sync/send/credential replacement
// and pins the same verified-mailbox token for preflight and the one write.
func (p *Provider) WithDraftSession(ctx context.Context, email string, run func(*googleapi.API) error) error {
	p.gate.Lock()
	defer p.gate.Unlock()
	session, err := p.sendSession(ctx, email)
	if err != nil {
		return err
	}
	return run(session)
}
