package oauth

import (
	"os"
	"strings"

	"golang.org/x/oauth2"
)

// Values are injected with -X when packaging. Process values always win,
// including explicit empty values used to disable a packaged client.
var googleClientID, googleClientSecret string
var microsoftClientID, microsoftClientSecret string

func configured(name, built string) string {
	if value, ok := os.LookupEnv(name); ok {
		return strings.TrimSpace(value)
	}
	return built
}

func Google() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     configured("GOOGLE_CLIENT_ID", googleClientID),
		ClientSecret: configured("GOOGLE_CLIENT_SECRET", googleClientSecret),
		Scopes:       []string{"https://www.googleapis.com/auth/gmail.modify", "openid", "email", "profile"},
		Endpoint:     oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams},
	}
}

func Microsoft() *oauth2.Config {
	return &oauth2.Config{ClientID: configured("MICROSOFT_CLIENT_ID", microsoftClientID), ClientSecret: configured("MICROSOFT_CLIENT_SECRET", microsoftClientSecret)}
}
