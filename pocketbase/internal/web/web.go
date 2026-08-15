// Package web is GolfTrack's frontend: the page routes, the templates they
// render, and the static assets they load, all served by the PocketBase binary
// itself.
//
// This is Phase 7B of the migration. The pages it replaces were Django
// server-rendering — `rounds/views.py`, `courses/views.py` and eleven Django
// templates — which PocketBase cannot serve, so they are ported rather than
// re-pointed: same routes, same markup, same Alpine.js islands, same Tailwind
// classes. What changes underneath is where the data comes from (the domain
// packages directly, the same functions the JSON routes call) and how the
// request is authenticated (the auth cookie from AUTH.md, not a Django
// session).
//
// Three rules shape everything here:
//
//   - **Pages are GET-only.** Every write goes through the JSON API with an
//     `Authorization` header, from the page's own JavaScript. Nothing is
//     written on the strength of a cookie, so there is no CSRF surface to
//     replace Django's `{% csrf_token %}` with.
//   - **Gating is advisory in the UI and real on the server.** A page hides an
//     admin link and refuses an admin route, but the collection rules and the
//     route handlers are what actually enforce it.
//   - **Nothing is fetched from a CDN at run time.** Alpine and the PocketBase
//     SDK are vendored into the embedded static FS, because a PWA that needs
//     unpkg.com to boot is not much of a PWA.
package web

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/template"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
)

//go:embed all:templates
var templatesFS embed.FS

//go:embed all:static
var staticFS embed.FS

// server holds what every handler needs: the template registry (which caches
// parsed templates) and nothing else — the app comes in on the request event,
// so the same server works against a test app.
type server struct {
	registry *template.Registry
}

// Register mounts the frontend on the app. Called from main.go, not from
// internal/hooks: these are pages, not domain rules, and the split keeps
// "what the API does" and "what the browser sees" reviewable apart.
func Register(app core.App) {
	s := &server{registry: template.NewRegistry().AddFuncs(templateFuncs)}

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		static, err := fs.Sub(staticFS, "static")
		if err != nil {
			return err
		}
		e.Router.GET("/static/{path...}", apis.Static(static, false))
		e.Router.GET("/sw.js", s.serviceWorker)

		// Page routes, in the order the plan's table lists them. Paths carry
		// Django's trailing slash, and the slashless spelling redirects onto it
		// the way Django's APPEND_SLASH does, so a bookmark of either survives.
		e.Router.GET("/{$}", s.public(s.home))

		e.Router.GET("/courses/{$}", s.public(s.courseList))
		e.Router.GET("/courses/new/{$}", s.adminOnly(s.courseNew))
		e.Router.GET("/courses/archived/{$}", s.adminOnly(s.courseArchived))
		e.Router.GET("/courses/{id}/{$}", s.public(s.courseDetail))
		e.Router.GET("/courses/{id}/edit/{$}", s.adminOnly(s.courseEdit))

		e.Router.GET("/rounds/{$}", s.loggedIn(s.roundList))
		e.Router.GET("/rounds/new/{$}", s.loggedIn(s.roundNew))
		e.Router.GET("/rounds/{id}/{$}", s.loggedIn(s.roundDetail))
		e.Router.GET("/rounds/{id}/play/{$}", s.loggedIn(s.roundPlay))
		e.Router.GET("/rounds/{id}/review/{$}", s.loggedIn(s.roundReview))

		e.Router.GET("/settings/{$}", s.loggedIn(s.settings))
		e.Router.GET("/settings/export/{$}", s.loggedIn(s.settingsExport))
		e.Router.GET("/settings/import/{$}", s.loggedIn(s.settingsImport))

		e.Router.GET("/accounts/login/{$}", s.public(s.login))
		e.Router.GET("/accounts/logout/{$}", s.public(s.logout))

		for _, path := range []string{
			"/courses", "/courses/new", "/courses/archived", "/courses/{id}", "/courses/{id}/edit",
			"/rounds", "/rounds/new", "/rounds/{id}", "/rounds/{id}/play", "/rounds/{id}/review",
			"/settings", "/settings/export", "/settings/import",
			"/accounts/login", "/accounts/logout",
		} {
			e.Router.GET(path, appendSlash)
		}

		return e.Next()
	})
}

// handler is a page handler: it knows who is asking, which is the one thing
// every page needs and no PocketBase route signature carries.
type handler func(e *core.RequestEvent, v *viewer) error

// public runs a handler for anyone, signed in or not — Django's undecorated
// views (`home`, `course_list`, `course_detail`).
func (s *server) public(h handler) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		return s.run(e, currentViewer(e), h)
	}
}

// loggedIn ports @login_required_view: a signed-out visitor is redirected to
// sign-in, carrying where they were going.
func (s *server) loggedIn(h handler) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		v := currentViewer(e)
		if !v.SignedIn() {
			return e.Redirect(http.StatusFound, loginPath(e.Request.URL.Path))
		}
		return s.run(e, v, h)
	}
}

// adminOnly ports @admin_required: signed out is a redirect, signed in without
// the role is Django's 403.
func (s *server) adminOnly(h handler) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		v := currentViewer(e)
		if !v.SignedIn() {
			return e.Redirect(http.StatusFound, loginPath(e.Request.URL.Path))
		}
		if !v.IsAdmin() {
			return s.renderError(e, v, http.StatusForbidden)
		}
		return s.run(e, v, h)
	}
}

// run invokes a handler and turns whatever it returns into a page: a 404 for
// the contract's "not found", a logged 500 for anything unexpected. A page
// never leaks an internal error message the way a JSON route's 500 body would
// not either.
func (s *server) run(e *core.RequestEvent, v *viewer, h handler) error {
	err := h(e, v)
	if err == nil {
		return nil
	}

	var apiErr *apierr.Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return s.renderError(e, v, http.StatusNotFound)
	}

	e.App.Logger().Error("GolfTrack page failed", "url", e.Request.URL.Path, "error", err)

	return s.renderError(e, v, http.StatusInternalServerError)
}

// appendSlash is Django's APPEND_SLASH for the slashless spelling of a page.
func appendSlash(e *core.RequestEvent) error {
	target := e.Request.URL.Path + "/"
	if e.Request.URL.RawQuery != "" {
		target += "?" + e.Request.URL.RawQuery
	}
	return e.Redirect(http.StatusMovedPermanently, target)
}

// render executes a page template inside the base layout and writes it.
//
// The layout is always the first file, because template.Registry executes the
// first template it was given and the page's `{{define}}` blocks fill in the
// holes the layout leaves — Go's equivalent of `{% extends "base.html" %}`.
func (s *server) render(e *core.RequestEvent, status int, page string, data any) error {
	html, err := s.registry.LoadFS(templatesFS, "templates/base.html", "templates/"+page).Render(data)
	if err != nil {
		e.App.Logger().Error("GolfTrack template failed", "template", page, "error", err)
		if status != http.StatusInternalServerError {
			return e.HTML(http.StatusInternalServerError, "Internal server error")
		}
		// The error page itself is broken; anything more elaborate would
		// recurse.
		return e.HTML(http.StatusInternalServerError, "Internal server error")
	}

	return e.HTML(status, html)
}

// renderError draws the 403/404/500 pages, which share a template and differ
// only in what they say.
func (s *server) renderError(e *core.RequestEvent, v *viewer, status int) error {
	message, heading := "Something went wrong on our end.", "Something broke"
	switch status {
	case http.StatusNotFound:
		heading, message = "Not found", "That page does not exist, or is not yours to see."
	case http.StatusForbidden:
		heading, message = "Forbidden", "You need an admin account to open that page."
	}

	return s.render(e, status, "error.html", &errorPage{
		Layout:  s.layout(e, v, heading+" — GolfTrack"),
		Status:  status,
		Heading: heading,
		Message: message,
	})
}

// serviceWorker serves /sw.js from the embedded FS with the
// Service-Worker-Allowed header set to "/" so the worker controls the whole
// origin even though the file is embedded alongside the other static assets.
func (s *server) serviceWorker(e *core.RequestEvent) error {
	data, err := staticFS.ReadFile("static/js/sw.js")
	if err != nil {
		return err
	}
	e.Response.Header().Set("Service-Worker-Allowed", "/")
	return e.Blob(http.StatusOK, "application/javascript; charset=utf-8", data)
}
