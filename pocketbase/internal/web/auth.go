package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// The auth-cookie contract, implementing the decision recorded in AUTH.md
// § "For the frontend" (#128): the session is a token the client holds, stored
// in a cookie rather than localStorage so that the *server* can gate a page
// render before any JavaScript runs.
//
// The cookie is written by the browser through the PocketBase JS SDK's
// `authStore.exportToCookie()`, which serialises `{"token": …, "record": {…}}`
// as a URL-encoded JSON string under the name below. Everything here reads that
// cookie; nothing here writes it, because the token is only ever minted by
// PocketBase's own auth endpoints.
//
// **Only the token is trusted.** The `record` half of the cookie is written by
// the client and can say anything — including `"role": "ADMIN"`. It is ignored:
// the token is verified against the instance's signing key and the record is
// re-read from the database, so a hand-edited cookie gains nothing.
// `TestTamperedCookieRecordCannotGrantAdmin` holds that line.
const authCookieName = "pb_auth"

// viewer is who the request is from, as far as a page render is concerned:
// nil-safe, so a template can ask about a signed-out visitor without a branch
// at every call site.
type viewer struct {
	Record *core.Record
}

// SignedIn reports whether the request carries a valid session.
func (v *viewer) SignedIn() bool { return v != nil && v.Record != nil }

// IsAdmin reports whether the viewer may see the admin-only UI. It reads the
// role off the *database* record, never off the cookie.
//
// This is the same advisory-only flag AUTH.md describes: it decides what the
// nav renders, while the server rules and the route gating decide what actually
// happens.
func (v *viewer) IsAdmin() bool {
	if !v.SignedIn() {
		return false
	}
	if v.Record.IsSuperuser() {
		return true
	}
	return v.Record.GetString(collections.FieldRole) == collections.UserRoleAdmin
}

// ID returns the viewer's record id, or "" when signed out.
func (v *viewer) ID() string {
	if !v.SignedIn() {
		return ""
	}
	return v.Record.Id
}

// Email returns the viewer's email, or "" when signed out.
func (v *viewer) Email() string {
	if !v.SignedIn() {
		return ""
	}
	return v.Record.GetString("email")
}

// authCookiePayload is the JSON the SDK writes. `record` is deliberately absent
// from this struct: see the note on authCookieName.
type authCookiePayload struct {
	Token string `json:"token"`
}

// currentViewer resolves the request's session.
//
// The cookie is the contract, and is checked first. An `Authorization` header
// is honoured as well when PocketBase's own middleware has already resolved it
// — that costs nothing here and means a page can be fetched by anything that
// talks to the API (curl, a test) without minting a cookie first.
//
// Any failure — no cookie, malformed cookie, expired or forged token, a token
// for a record that has since been deleted — resolves to a signed-out viewer
// rather than an error. A page has somewhere to send a signed-out visitor; it
// has nothing useful to say about a broken cookie.
func currentViewer(e *core.RequestEvent) *viewer {
	if token := cookieToken(e.Request); token != "" {
		if record, err := e.App.FindAuthRecordByToken(token, core.TokenTypeAuth); err == nil {
			return &viewer{Record: record}
		}
	}

	if e.Auth != nil {
		return &viewer{Record: e.Auth}
	}

	return &viewer{}
}

// cookieToken pulls the token out of the SDK's cookie.
func cookieToken(r *http.Request) string {
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return ""
	}

	// The SDK writes the value with encodeURIComponent. PathUnescape, rather
	// than QueryUnescape, because the latter also turns "+" into a space —
	// harmless for a base64url JWT but wrong for anything else in the payload.
	raw, err := url.PathUnescape(cookie.Value)
	if err != nil {
		raw = cookie.Value
	}

	var payload authCookiePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}

	return payload.Token
}

// loginPath is where a signed-out visitor is sent, carrying the page they were
// trying to reach — Django's `redirect(f"/accounts/login/?next={request.path}")`.
func loginPath(next string) string {
	if next == "" || next == "/" {
		return "/accounts/login/"
	}
	return "/accounts/login/?next=" + url.QueryEscape(next)
}

// safeNext sanitises the `next` parameter before it becomes a redirect.
//
// Only a path on this origin is allowed: anything scheme-relative ("//evil")
// or absolute ("https://evil") is discarded in favour of the home page, so the
// sign-in page cannot be turned into an open redirect.
func safeNext(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	return raw
}
