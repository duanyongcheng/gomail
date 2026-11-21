package desktop

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/duanyongcheng/pebble-go/internal/app"
	"golang.org/x/net/idna"
)

type ExternalURLRequest struct {
	URL string `json:"url"`
}

func (s *AppService) OpenExternal(request ExternalURLRequest) (app.Empty, error) {
	if !validExternalURL(request.URL) {
		return app.Empty{}, errors.New("only valid HTTP or HTTPS links without credentials can be opened")
	}
	if s.OpenURL == nil {
		return app.Empty{}, errors.New("external links are unavailable")
	}
	if err := s.OpenURL(request.URL); err != nil {
		// OS errors can include the complete URL, including private query tokens.
		return app.Empty{}, errors.New("could not open the link in your default browser")
	}
	return app.Empty{}, nil
}

func validExternalURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 64<<10 || !utf8.ValidString(raw) {
		return false
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\\' {
			return false
		}
	}
	// net/url leaves the query escaped. Check every escape, including query and
	// fragment, before handing the original URL to a different platform parser.
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) {
		return false
	}
	for _, r := range decoded {
		if unicode.IsControl(r) || r == '\\' {
			return false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" ||
		(!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
		return false
	}
	if strings.Contains(u.Host, "%") || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return false
		}
	}
	host := u.Hostname()
	if strings.HasPrefix(u.Host, "[") || strings.Contains(host, ":") {
		ip, err := netip.ParseAddr(host)
		return err == nil && ip.Is6() && strings.HasPrefix(u.Host, "[")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Is4()
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return false
	}
	ascii = strings.TrimSuffix(ascii, ".")
	if len(ascii) == 0 || len(ascii) > 253 {
		return false
	}
	for _, label := range strings.Split(ascii, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}
