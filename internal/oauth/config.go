package oauth

import (
	"os"
	"strings"

	"golang.org/x/oauth2"
)

func configured(name string) string {
	if value, ok := os.LookupEnv(name); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func GoogleWithCredentials(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     strings.TrimSpace(clientID),
		ClientSecret: strings.TrimSpace(clientSecret),
		Scopes:       []string{"https://www.googleapis.com/auth/gmail.modify", "openid", "email", "profile"},
		Endpoint:     oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams},
	}
}

func Google() *oauth2.Config {
	return GoogleWithCredentials(
		configured("GOOGLE_CLIENT_ID"),
		configured("GOOGLE_CLIENT_SECRET"),
	)
}

func GoogleFallbackSource() string {
	if configured("GOOGLE_CLIENT_ID") != "" {
		return "env"
	}
	return "none"
}

func Microsoft() *oauth2.Config {
	return &oauth2.Config{ClientID: configured("MICROSOFT_CLIENT_ID"), ClientSecret: configured("MICROSOFT_CLIENT_SECRET")}
}
