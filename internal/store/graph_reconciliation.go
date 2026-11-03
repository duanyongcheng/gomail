package store

import (
	"context"
	"errors"
)

// MissingProviderMappingsPage compares an old folder membership with the durable
// seen set of a replacement scan without retaining the mailbox in memory.
func (db *DB) MissingProviderMappingsPage(ctx context.Context, account, provider, scope, seenScope, after string, limit int) ([]ProviderMapping, error) {
	if limit < 1 || limit > 1000 || scope == seenScope {
		return nil, errors.New("invalid provider reconciliation page")
	}
	rows, err := db.read.QueryContext(ctx, `SELECT m.remote_id,m.message_id FROM provider_mappings m WHERE m.account_id=? AND m.provider=? AND m.scope=? AND m.remote_id>? AND NOT EXISTS (SELECT 1 FROM provider_mappings seen WHERE seen.account_id=m.account_id AND seen.provider=m.provider AND seen.scope=? AND seen.remote_id=m.remote_id) ORDER BY m.remote_id LIMIT ?`, account, provider, scope, after, seenScope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderMapping{}
	for rows.Next() {
		m := ProviderMapping{Provider: provider, Scope: scope}
		if err := rows.Scan(&m.RemoteID, &m.MessageID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
