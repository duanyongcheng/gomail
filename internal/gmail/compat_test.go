// Package gmail retains the tested, isolated protocol prototype. Product code
// uses provider/gmail and the shared googleapi/oauth packages.
package gmail

import (
	"github.com/duanyongcheng/pebble-go/internal/googleapi"
	"github.com/duanyongcheng/pebble-go/internal/oauth"
	"github.com/duanyongcheng/pebble-go/internal/testgmail"
)

const APIBase = googleapi.APIBase
const ReadScope = oauth.ReadScope
const ModifyScope = oauth.ModifyScope

type TokenSource = googleapi.TokenSource
type API = googleapi.API
type HTTPError = googleapi.HTTPError
type Profile = googleapi.Profile
type Label = googleapi.Label
type Ref = googleapi.Ref
type Message = googleapi.Message
type ListPage = googleapi.ListPage
type HistoryEntry = googleapi.HistoryEntry
type HistoryPage = googleapi.HistoryPage
type Tokens = oauth.Tokens
type Mock = testgmail.Mock

var NewMock = testgmail.NewMock

var LoadDesktop = oauth.LoadDesktop
var Authorize = oauth.Authorize
var NewTokens = oauth.NewTokens
