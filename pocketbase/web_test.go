package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/web"
)

// Phase 7B page-route suite.
//
// The pages are a port of Django's, so this suite asks the questions
// tests/test_views.py asks of the originals: does every route render, does a
// signed-out visitor get bounced to sign-in, does a non-admin get refused, and
// does a round belonging to somebody else 404 rather than render.
//
// Two things here are specific to the PocketBase build and are the reason the
// suite is not just a translation:
//
//   - the session is a cookie the *client* writes, so the tests write one too,
//     and one test writes a deliberately lying one;
//   - the pages must not reach a CDN at run time, which is a property of the
//     rendered HTML and therefore testable.

// runWeb is runPlay with the frontend registered and the session carried in a
// cookie rather than an Authorization header — the way a browser does it.
func runWeb(t *testing.T, who func(*playFixture) *core.Record, s tests.ApiScenario) {
	t.Helper()

	s.DisableTestAppCleanup = true
	s.TestAppFactory = func(tb testing.TB) *tests.TestApp {
		f := newPlayFixture(tb)
		web.Register(f.app)
		if who != nil {
			s.Headers = authCookie(tb, who(f))
		}
		return f.app
	}
	s.Test(t)
}

// authCookie builds the `pb_auth` cookie the PocketBase JS SDK writes:
// URL-encoded JSON holding the token and a copy of the record.
func authCookie(t testing.TB, record *core.Record) map[string]string {
	t.Helper()

	if record == nil {
		return nil
	}

	token, err := record.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token for %s: %v", record.Id, err)
	}

	return map[string]string{"Cookie": "pb_auth=" + url.QueryEscape(
		`{"token":"`+token+`","record":{"id":"`+record.Id+`"}}`,
	)}
}

// -----------------------------------------------------------------------------
// every page loads
// -----------------------------------------------------------------------------

// TestPagesLoadForASignedInPlayer is the gate item: every route in the plan's
// page table renders for a signed-in user, with the content that page is for.
func TestPagesLoadForASignedInPlayer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		url     string
		content []string
	}{
		{"home", "/", []string{"Track your round", "Continue round", "Play Links"}},
		{"courses list", "/courses/", []string{"Courses", "Play Links", "18 holes"}},
		{"course detail", "/courses/" + idPlayCourse + "/", []string{"Play Links", "Hole Pars", "Par 4"}},
		{"rounds list", "/rounds/", []string{"Rounds", "In Progress", "Continue"}},
		{"new round", "/rounds/new/", []string{"Start a new round", "Choose a course", "Play mode"}},
		{"round detail", "/rounds/" + idDoneRound + "/", []string{"Completed Round", "Delete Round", "Hole by hole"}},
		{"round play", "/rounds/" + idPlayRound + "/play/", []string{"round-init", "Add a shot", "Driver", "Review Round"}},
		{"round review", "/rounds/" + idPlayRound + "/review/", []string{"Review Round", "Round timing", "Times shown in America/New_York.", "Start time", "End time", "Round notes", "Submit Round"}},
		{"settings", "/settings/", []string{"Settings", "Import", "Export", "Sign Out"}},
		{"settings export", "/settings/export/", []string{"Export", "Export JSON", "Export CSV"}},
		{"settings import", "/settings/import/", []string{"Import", "Import file", "JSON template", "CSV template"}},
		{"sign out", "/accounts/logout/", []string{"Sign out?"}},
	} {
		runWeb(t, playOwner, tests.ApiScenario{
			Name:            tc.name + " loads",
			Method:          http.MethodGet,
			URL:             tc.url,
			ExpectedStatus:  200,
			ExpectedContent: tc.content,
		})
	}
}

// TestAdminPagesLoadForAnAdmin covers the three admin-only routes, which the
// player above cannot reach at all.
func TestAdminPagesLoadForAnAdmin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		url     string
		content []string
	}{
		{"new course", "/courses/new/", []string{"Add Course", "Course time zone (optional)", "<select id=\"timeZone\"", "If left blank, round times are shown in UTC.", "Number of holes", "Hole pars", "Save Course"}},
		{"edit course", "/courses/" + idPlayCourse9 + "/edit/", []string{"Edit Course", "Nine Acres", "Course time zone (optional)", "<select id=\"timeZone\"", "Save Changes"}},
		{"archived courses", "/courses/archived/", []string{"Archived Courses"}},
	} {
		runWeb(t, playAdmin, tests.ApiScenario{
			Name:            tc.name + " loads for an admin",
			Method:          http.MethodGet,
			URL:             tc.url,
			ExpectedStatus:  200,
			ExpectedContent: tc.content,
		})
	}
}

// TestSignInPageRendersTheConfiguredProviders is the sign-in page standing in
// for allauth's. The fixture configures no OAuth provider and no password
// login, so what it must render is the "no way to sign in" notice rather than a
// button that cannot work — the same closed baseline authconfig.go logs about
// at startup.
func TestSignInPageRendersTheConfiguredProviders(t *testing.T) {
	t.Parallel()
	runWeb(t, nil, tests.ApiScenario{
		Name:            "the sign-in page renders",
		Method:          http.MethodGet,
		URL:             "/accounts/login/",
		ExpectedStatus:  200,
		ExpectedContent: []string{"Sign in to GolfTrack", "no sign-in method configured"},
	})
}

// TestCoursePagesAreOpenToAnonymousVisitors matches Django exactly: neither
// course view carries a decorator, and `GET /api/courses` is open for the same
// reason. A migration that quietly closed these would be a behaviour change
// nobody asked for.
func TestCoursePagesAreOpenToAnonymousVisitors(t *testing.T) {
	t.Parallel()
	runWeb(t, nil, tests.ApiScenario{
		Name:               "an anonymous visitor can list courses",
		Method:             http.MethodGet,
		URL:                "/courses/",
		ExpectedStatus:     200,
		ExpectedContent:    []string{"Play Links", "Sign in"},
		NotExpectedContent: []string{"+ Add Course"},
	})

	runWeb(t, nil, tests.ApiScenario{
		Name:            "an anonymous visitor can open a course",
		Method:          http.MethodGet,
		URL:             "/courses/" + idPlayCourse + "/",
		ExpectedStatus:  200,
		ExpectedContent: []string{"Hole Pars"},
	})
}

// -----------------------------------------------------------------------------
// gating
// -----------------------------------------------------------------------------

// TestGatedPagesRedirectASignedOutVisitor ports @login_required_view, including
// the `?next=` that sends the visitor back where they were going.
func TestGatedPagesRedirectASignedOutVisitor(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/rounds/",
		"/rounds/new/",
		"/rounds/" + idPlayRound + "/",
		"/rounds/" + idPlayRound + "/play/",
		"/rounds/" + idPlayRound + "/review/",
		"/settings/",
		"/settings/export/",
		"/settings/import/",
		"/courses/new/",
		"/courses/archived/",
		"/courses/" + idPlayCourse + "/edit/",
	} {
		runWeb(t, nil, tests.ApiScenario{
			Name:           "signed out at " + path,
			Method:         http.MethodGet,
			URL:            path,
			ExpectedStatus: 302,
			AfterTestFunc: func(t testing.TB, _ *tests.TestApp, res *http.Response) {
				want := "/accounts/login/?next=" + url.QueryEscape(path)
				if got := res.Header.Get("Location"); got != want {
					t.Errorf("Location = %q, want %q", got, want)
				}
			},
		})
	}
}

// TestAdminPagesRefuseANonAdmin ports @admin_required's other half: a
// signed-in player is not redirected — they are refused, the way Django's
// HttpResponseForbidden does.
func TestAdminPagesRefuseANonAdmin(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/courses/new/",
		"/courses/archived/",
		"/courses/" + idPlayCourse + "/edit/",
	} {
		runWeb(t, playOwner, tests.ApiScenario{
			Name:               "a player at " + path,
			Method:             http.MethodGet,
			URL:                path,
			ExpectedStatus:     403,
			ExpectedContent:    []string{"Forbidden", "admin account"},
			NotExpectedContent: []string{"Save Course", "Save Changes"},
		})
	}
}

// TestRoundPagesDoNotLeakAnotherPlayersRound is ownership on the page routes.
// The rounds queries scope by user, so somebody else's round is a 404 page —
// never a render, and never a hint that the id exists.
func TestRoundPagesDoNotLeakAnotherPlayersRound(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/rounds/" + idPlayRound + "/",
		"/rounds/" + idPlayRound + "/play/",
		"/rounds/" + idPlayRound + "/review/",
	} {
		runWeb(t, playOther, tests.ApiScenario{
			Name:               "another player at " + path,
			Method:             http.MethodGet,
			URL:                path,
			ExpectedStatus:     404,
			ExpectedContent:    []string{"Not found"},
			NotExpectedContent: []string{"Play Links", "Add a shot"},
		})
	}
}

// TestUnknownPageIsA404Page covers the other 404: a path that is not a route
// at all still lands on GolfTrack's page rather than PocketBase's JSON.
func TestUnknownPageIsA404Page(t *testing.T) {
	t.Parallel()
	runWeb(t, playOwner, tests.ApiScenario{
		Name:            "an unknown course id",
		Method:          http.MethodGet,
		URL:             "/courses/" + fixedID("nocourse") + "/",
		ExpectedStatus:  404,
		ExpectedContent: []string{"Not found"},
	})
}

// -----------------------------------------------------------------------------
// the auth cookie
// -----------------------------------------------------------------------------

// TestTamperedCookieRecordCannotGrantAdmin is the security property the cookie
// design rests on. The cookie is written by the client and carries a copy of
// the user record, so a player can hand-edit `"role":"ADMIN"` into it. The
// server ignores that half entirely: the token is verified and the role is
// re-read from the database, so the admin page still refuses them.
func TestTamperedCookieRecordCannotGrantAdmin(t *testing.T) {
	t.Parallel()
	scenario := tests.ApiScenario{
		Name:               "a cookie claiming ADMIN does not open the admin page",
		Method:             http.MethodGet,
		URL:                "/courses/new/",
		ExpectedStatus:     403,
		ExpectedContent:    []string{"Forbidden"},
		NotExpectedContent: []string{"Save Course"},
	}
	scenario.DisableTestAppCleanup = true
	scenario.TestAppFactory = func(tb testing.TB) *tests.TestApp {
		f := newPlayFixture(tb)
		web.Register(f.app)

		token, err := f.owner.NewAuthToken()
		if err != nil {
			tb.Fatalf("auth token: %v", err)
		}
		payload, err := json.Marshal(map[string]any{
			"token": token,
			// The lie: this player's stored role is USER.
			"record": map[string]any{"id": f.owner.Id, "role": "ADMIN"},
		})
		if err != nil {
			tb.Fatalf("encode cookie: %v", err)
		}
		scenario.Headers = map[string]string{
			"Cookie": "pb_auth=" + url.QueryEscape(string(payload)),
		}

		return f.app
	}
	scenario.Test(t)
}

// TestGarbageCookieIsTreatedAsSignedOut: a malformed or forged cookie resolves
// to "no session" rather than to an error page, because a signed-out visitor is
// something every page already knows how to handle.
func TestGarbageCookieIsTreatedAsSignedOut(t *testing.T) {
	t.Parallel()
	for name, cookie := range map[string]string{
		"not JSON":       "pb_auth=" + url.QueryEscape("nonsense"),
		"no token":       "pb_auth=" + url.QueryEscape(`{"record":{"id":"x"}}`),
		"forged token":   "pb_auth=" + url.QueryEscape(`{"token":"not.a.jwt"}`),
		"empty envelope": "pb_auth=" + url.QueryEscape(`{}`),
	} {
		runWeb(t, nil, tests.ApiScenario{
			Name:            "a " + name + " cookie is signed out",
			Method:          http.MethodGet,
			URL:             "/rounds/",
			Headers:         map[string]string{"Cookie": cookie},
			ExpectedStatus:  302,
			ExpectedContent: []string{},
		})
	}
}

// -----------------------------------------------------------------------------
// assets
// -----------------------------------------------------------------------------

// TestStaticAssetsAreServedFromTheBinary covers the Phase 9 gate item early:
// the CSS, the vendored scripts, the manifest and the icons all come out of the
// embedded FS, with no filesystem or CDN involved.
func TestStaticAssetsAreServedFromTheBinary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path    string
		content []string
	}{
		{"/static/css/app.css", []string{".bg-emerald-700"}},
		{"/static/js/alpine.min.js", []string{"Alpine"}},
		{"/static/js/pocketbase.umd.js", []string{"PocketBase"}},
		{"/static/js/golftrack.js", []string{"window.gt", "timeZone || 'UTC'", "Intl.supportedValuesOf('timeZone')", "UTC (not set)", "America/New_York", "serviceWorker", "/sw.js"}},
		{"/static/js/round-play.js", []string{"roundPlay", "golftrack:offline:", "syncQueue", "hasPendingOnCurrentHole", "pending: true"}},
		{"/static/manifest.webmanifest", []string{"GolfTrack", "standalone"}},
	} {
		runWeb(t, nil, tests.ApiScenario{
			Name:            tc.path + " is served",
			Method:          http.MethodGet,
			URL:             tc.path,
			ExpectedStatus:  200,
			ExpectedContent: tc.content,
		})
	}
}

// TestIconsAreServedFromTheBinary covers the logo/icon refresh: the favicons,
// the apple touch icon and the (regular + maskable) PWA icons referenced from
// base.html and the manifest all resolve from the embedded FS.
func TestIconsAreServedFromTheBinary(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/static/icons/favicon-16.png",
		"/static/icons/favicon-32.png",
		"/static/icons/apple-touch-icon.png",
		"/static/icons/icon-192.png",
		"/static/icons/icon-192-maskable.png",
		"/static/icons/icon-512.png",
		"/static/icons/icon-512-maskable.png",
	} {
		runWeb(t, nil, tests.ApiScenario{
			Name:            path + " is served",
			Method:          http.MethodGet,
			URL:             path,
			ExpectedStatus:  200,
			ExpectedContent: []string{"PNG"},
		})
	}
}

// TestServiceWorkerIsServed checks that /sw.js is served with the
// Service-Worker-Allowed header set to "/" so the worker can control the whole
// origin, and that it contains the expected cache strategies.
func TestServiceWorkerIsServed(t *testing.T) {
	runWeb(t, nil, tests.ApiScenario{
		Name:            "/sw.js is served with correct content and headers",
		Method:          http.MethodGet,
		URL:             "/sw.js",
		ExpectedStatus:  200,
		ExpectedContent: []string{"golftrack-v1", "PRECACHE", "/static/css/app.css", "navigate"},
		AfterTestFunc: func(t testing.TB, _ *tests.TestApp, res *http.Response) {
			got := res.Header.Get("Service-Worker-Allowed")
			if got != "/" {
				t.Errorf("Service-Worker-Allowed = %q, want %q", got, "/")
			}
		},
	})
}

// TestOfflineBannerIsInBaseLayout checks that every page carries the Alpine
// offline banner so golfers know their score is not yet synced when there is
// no network connection.
func TestOfflineBannerIsInBaseLayout(t *testing.T) {
	runWeb(t, playOwner, tests.ApiScenario{
		Name:            "offline banner is present on the play page",
		Method:          http.MethodGet,
		URL:             "/rounds/" + idPlayRound + "/play/",
		ExpectedStatus:  200,
		ExpectedContent: []string{"Offline", "sync when you reconnect", "navigator.onLine", "@online.window", "@offline.window"},
	})
}

// TestPagesLoadNothingFromACDN is the "no run-time requests to any external
// CDN" gate item, asserted where it can regress: in the rendered HTML. The
// Django base template pulled Alpine and htmx from unpkg.com, which an
// installed PWA cannot rely on.
func TestPagesLoadNothingFromACDN(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/", "/courses/", "/rounds/", "/rounds/" + idPlayRound + "/play/"} {
		runWeb(t, playOwner, tests.ApiScenario{
			Name:            "no external origins on " + path,
			Method:          http.MethodGet,
			URL:             path,
			ExpectedStatus:  200,
			ExpectedContent: []string{"/static/"},
			NotExpectedContent: []string{
				"unpkg.com", "cdn.jsdelivr.net", "cdnjs.cloudflare.com", "https://cdn.",
			},
		})
	}

	// The sign-in page is the one that loads the SDK, and it only renders for a
	// visitor who is not signed in yet.
	runWeb(t, nil, tests.ApiScenario{
		Name:            "no external origins on /accounts/login/",
		Method:          http.MethodGet,
		URL:             "/accounts/login/",
		ExpectedStatus:  200,
		ExpectedContent: []string{"/static/js/pocketbase.umd.js"},
		NotExpectedContent: []string{
			"unpkg.com", "cdn.jsdelivr.net", "cdnjs.cloudflare.com", "https://cdn.",
		},
	})
}

// TestPagesCarryNoCSRFPlumbing pins the third cross-cutting item: Django's
// `<meta name="csrf-token">` and its uses are gone, because auth is a bearer
// token and no page route accepts a write.
func TestPagesCarryNoCSRFPlumbing(t *testing.T) {
	t.Parallel()
	runWeb(t, playOwner, tests.ApiScenario{
		Name:               "the play page has no CSRF token",
		Method:             http.MethodGet,
		URL:                "/rounds/" + idPlayRound + "/play/",
		ExpectedStatus:     200,
		NotExpectedContent: []string{"csrf", "csrfmiddlewaretoken"},
		ExpectedContent:    []string{"round-init"},
	})
}

// TestSlashlessPathsRedirect is Django's APPEND_SLASH, kept so that a link or
// bookmark written without the trailing slash still lands.
func TestSlashlessPathsRedirect(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/courses", "/rounds", "/settings", "/settings/export", "/settings/import", "/accounts/login"} {
		runWeb(t, playOwner, tests.ApiScenario{
			Name:           path + " redirects onto the trailing slash",
			Method:         http.MethodGet,
			URL:            path,
			ExpectedStatus: 301,
			AfterTestFunc: func(t testing.TB, _ *tests.TestApp, res *http.Response) {
				if got := res.Header.Get("Location"); got != path+"/" {
					t.Errorf("Location = %q, want %q", got, path+"/")
				}
			},
		})
	}
}

// TestPlayPageBootstrapsTheRoundAsJSON checks the one piece of the port that is
// neither markup nor a query: the Alpine component boots from a JSON blob of
// the round, and it has to be the same RoundDetailOut shape the API returns or
// round-play.js reads the wrong field names.
func TestPlayPageBootstrapsTheRoundAsJSON(t *testing.T) {
	t.Parallel()
	runWeb(t, playOwner, tests.ApiScenario{
		Name:           "the play page embeds the round",
		Method:         http.MethodGet,
		URL:            "/rounds/" + idPlayRound + "/play/",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`id="round-init"`, `"currentHole":1`, `"holeNumber":3`, `"club":"Driver"`,
			`"totalStrokes":null`,
		},
		NotExpectedContent: []string{`"current_hole"`, `"hole_number"`},
	})
}

// TestHomePageShowsTheResumableRound is the home page's whole job, and the one
// place `null` totals reach a template: a round in progress has no score to
// show, and must not render as even par.
func TestHomePageShowsTheResumableRound(t *testing.T) {
	t.Parallel()
	runWeb(t, playOwner, tests.ApiScenario{
		Name:            "home offers the in-progress round",
		Method:          http.MethodGet,
		URL:             "/",
		ExpectedStatus:  200,
		ExpectedContent: []string{"Play Links", "Hole 1", "/rounds/" + idPlayRound + "/play/"},
	})

	runWeb(t, playOther, tests.ApiScenario{
		Name:            "home tells a player with no round where to start",
		Method:          http.MethodGet,
		URL:             "/",
		ExpectedStatus:  200,
		ExpectedContent: []string{"No round in progress", "No completed rounds yet"},
	})
}

// TestTemplatesRenderEveryPageWithoutAnErrorPage is the blunt regression guard
// on the template set: a missing field or a renamed one shows up as the 500
// page, which no page test above would otherwise notice if it only checked for
// a string that happens to be in the layout too.
func TestTemplatesRenderEveryPageWithoutAnErrorPage(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/", "/courses/", "/courses/" + idPlayCourse + "/", "/rounds/", "/rounds/new/",
		"/rounds/" + idDoneRound + "/", "/rounds/" + idPlayRound + "/play/",
		"/rounds/" + idPlayRound + "/review/", "/settings/", "/settings/export/",
		"/settings/import/", "/accounts/logout/",
	} {
		runWeb(t, playOwner, tests.ApiScenario{
			Name:               "no error page at " + path,
			Method:             http.MethodGet,
			URL:                path,
			ExpectedStatus:     200,
			NotExpectedContent: []string{"Something broke", "Internal server error"},
			ExpectedContent:    []string{"GolfTrack"},
		})
	}
}
