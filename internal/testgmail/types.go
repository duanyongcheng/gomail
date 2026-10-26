// Package testgmail provides the synthetic OAuth/API server for tests and the
// explicit desktop selftest. It has no desktop or persistence dependency.
package testgmail

import (
	"github.com/bary/pebble-go/internal/googleapi"
	"github.com/bary/pebble-go/internal/oauth"
)

const ModifyScope = oauth.ModifyScope

type TokenSource = googleapi.TokenSource
type API = googleapi.API
type Profile = googleapi.Profile
type Label = googleapi.Label
type Ref = googleapi.Ref
type Message = googleapi.Message
type ListPage = googleapi.ListPage
type HistoryEntry = googleapi.HistoryEntry
type HistoryPage = googleapi.HistoryPage
