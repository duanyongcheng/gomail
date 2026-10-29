package oauth

import (
	"strings"

	"github.com/bary/pebble-go/internal/domain"
	"golang.org/x/oauth2"
)

// ValidateGmailGrant checks actual returned scopes, not requested scopes. Older
// stored grants remain readable; new authorizations must prove mail write access.
func ValidateGmailGrant(token *oauth2.Token) error {
	if token == nil || token.AccessToken == "" || token.RefreshToken == "" {
		return &domain.AccessError{State: domain.ConnectionReauthRequired}
	}
	scope, _ := token.Extra("scope").(string)
	for _, value := range strings.Fields(scope) {
		if value == ModifyScope || value == "https://mail.google.com/" {
			return nil
		}
	}
	return &domain.AccessError{State: domain.ConnectionPermissionRequired}
}

// Old envelopes have no scope metadata, and the isolated read-only prototype
// does not request write access. Production Gmail configs request modify.
func (t *Tokens) validateGrant(token *oauth2.Token) error {
	if token.Extra("scope") == nil {
		return nil
	}
	for _, scope := range t.cfg.Scopes {
		if scope == ModifyScope || scope == "https://mail.google.com/" {
			return ValidateGmailGrant(token)
		}
	}
	return nil
}
