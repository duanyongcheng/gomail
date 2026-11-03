package domain

import "github.com/bary/pebble-go/internal/mailnet"

// ProtocolConfig is a secret-bearing vault record. Never return it to a UI.
type ProtocolConfig struct {
	Version         int                  `json:"version"`
	IMAP            ProtocolEndpoint     `json:"imap"`
	SMTP            ProtocolEndpoint     `json:"smtp"`
	POP3            ProtocolEndpoint     `json:"pop3"`
	SelectedFolders []string             `json:"selected_folders,omitempty"`
	Proxy           mailnet.ProxySetting `json:"proxy"`
	Microsoft       *MicrosoftConfig     `json:"microsoft,omitempty"`
}

type ProtocolEndpoint struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Security  string `json:"security"`
	RootCAPEM string `json:"root_ca_pem,omitempty"`
	Username  string `json:"username"`
	Password  string `json:"password,omitempty"`
	AuthMode  string `json:"auth_mode"`
}

func (e ProtocolEndpoint) Endpoint() mailnet.Endpoint {
	return mailnet.Endpoint{Host: e.Host, Port: e.Port, Security: e.Security, RootCAPEM: e.RootCAPEM}
}

// Microsoft stores independent resource grants; exchanging one never replaces another.
type MicrosoftConfig struct {
	Audience   string                   `json:"audience"`
	Graph      bool                     `json:"graph"`
	IMAP       bool                     `json:"imap"`
	SMTP       bool                     `json:"smtp"`
	Generation uint64                   `json:"generation"`
	ClientID   string                   `json:"client_id"`
	Tenant     string                   `json:"tenant"`
	Subject    string                   `json:"subject"`
	Grants     map[string]ResourceGrant `json:"grants"`
}
type ResourceGrant struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	Expiry       int64    `json:"expiry"`
	Scopes       []string `json:"scopes"`
}
