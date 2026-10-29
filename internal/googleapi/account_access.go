package googleapi

import (
	"errors"

	"github.com/bary/pebble-go/internal/domain"
)

func (a *API) checkAccess() error {
	if a.BeforeRequest != nil {
		return a.BeforeRequest()
	}
	return nil
}
func (a *API) report(err error) {
	if a.OnResult != nil {
		a.OnResult(err)
	}
}

// ConnectionFailure does not mistake quota, policy errors, malformed mail, or
// cancellation for a revoked grant. Unknown403 reasons keep normal error handling.
func ConnectionFailure(err error) domain.ConnectionState {
	if err == nil {
		return domain.ConnectionAvailable
	}
	var access *domain.AccessError
	if errors.As(err, &access) {
		return access.State
	}
	var remote *HTTPError
	if !errors.As(err, &remote) {
		return ""
	}
	if remote.Status == 401 {
		return domain.ConnectionReauthRequired
	}
	if remote.Status == 403 && !remote.hasQuotaReason() {
		for _, reason := range remote.Reasons {
			if reason == "insufficientPermissions" || reason == "ACCESS_TOKEN_SCOPE_INSUFFICIENT" {
				return domain.ConnectionPermissionRequired
			}
		}
	}
	if remote.Status == 429 || remote.Status >= 500 || remote.hasQuotaReason() {
		return domain.ConnectionTemporaryError
	}
	return ""
}
