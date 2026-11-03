package domain

// ConnectionState describes account access, not mailbox freshness or delivery.
type ConnectionState string

const (
	ConnectionAvailable              ConnectionState = "available"
	ConnectionTemporaryError         ConnectionState = "temporary_error"
	ConnectionReauthRequired         ConnectionState = "reauth_required"
	ConnectionPermissionRequired     ConnectionState = "permission_required"
	ConnectionCredentialsUnavailable ConnectionState = "credentials_unavailable"
	ConnectionConfigurationRequired  ConnectionState = "configuration_required"
	ConnectionReconnecting           ConnectionState = "reconnecting"
)

func (s ConnectionState) Blocked() bool {
	return s != ConnectionAvailable && s != ConnectionTemporaryError
}

// AccessError carries only a static status; provider responses and tokens never
// cross the service boundary in authorization diagnostics.
type AccessError struct{ State ConnectionState }

func (e *AccessError) Error() string {
	switch e.State {
	case ConnectionReauthRequired:
		return "Mail authorization expired or was revoked; reconnect this account."
	case ConnectionPermissionRequired:
		return "Mail permission is required; reconnect and grant access to this resource."
	case ConnectionCredentialsUnavailable:
		return "Saved credentials are unavailable; unlock the profile keyring and reconnect."
	case ConnectionConfigurationRequired:
		return "Account authorization configuration requires attention."
	case ConnectionReconnecting:
		return "Account reconnection is in progress; queued work is retained."
	default:
		return "Mail connection failed temporarily; retry is scheduled."
	}
}
