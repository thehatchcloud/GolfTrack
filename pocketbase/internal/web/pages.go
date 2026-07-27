package web

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/courses"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/rounds"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/scoring"
)

// The page handlers, one per Django view, in the same order as courses/urls.py
// and rounds/urls.py.
//
// Each one is a query plus a template. The queries are the domain packages'
// exported reads — the same functions the JSON routes call — so a page and its
// API endpoint cannot answer differently, and a rule change lands on both at
// once.

// Layout is what every page needs from the request regardless of what it
// renders: the title, the path (for `next=` on a sign-in redirect) and who is
// looking.
type Layout struct {
	Title  string
	Path   string
	Viewer *viewer
}

func (s *server) layout(e *core.RequestEvent, v *viewer, title string) Layout {
	return Layout{Title: title, Path: e.Request.URL.Path, Viewer: v}
}

type errorPage struct {
	Layout
	Status  int
	Heading string
	Message string
}

// -----------------------------------------------------------------------------
// home
// -----------------------------------------------------------------------------

type homePage struct {
	Layout
	InProgress *rounds.DetailOut
	Recent     []*rounds.Out
}

// home ports core/views.home: the hero for everyone, and for a signed-in
// player the round they can resume plus their three most recent.
func (s *server) home(e *core.RequestEvent, v *viewer) error {
	page := &homePage{Layout: s.layout(e, v, "GolfTrack — Track your round")}

	if v.SignedIn() {
		inProgress, err := rounds.InProgress(e.App, v.ID())
		if err != nil {
			return err
		}
		completed, err := rounds.ListCompleted(e.App, v.ID())
		if err != nil {
			return err
		}
		if len(completed) > 3 {
			completed = completed[:3]
		}
		page.InProgress = inProgress
		page.Recent = completed
	}

	return s.render(e, http.StatusOK, "home.html", page)
}

// -----------------------------------------------------------------------------
// courses
// -----------------------------------------------------------------------------

type courseListPage struct {
	Layout
	Courses  []*courses.Out
	Archived bool
}

type courseDetailPage struct {
	Layout
	Course *courses.Out
}

// holeRow is one row of the course form: eighteen are always rendered and
// Alpine hides the ones past the selected hole count, exactly as
// courses/views.py's _build_hole_rows does.
type holeRow struct {
	HoleNumber int
	Par        int
}

type courseFormPage struct {
	Layout
	Course     *courses.Out
	Editing    bool
	HoleCount  int
	HoleRows   []holeRow
	ParOptions []int
}

const (
	maxHoles   = 18
	defaultPar = 4
)

var parOptions = []int{3, 4, 5, 6}

// courseList ports courses/views.course_list. It is deliberately not gated:
// the Django view carries no decorator, so a signed-out visitor can browse
// courses, and `GET /api/courses` is open for the same reason.
func (s *server) courseList(e *core.RequestEvent, v *viewer) error {
	list, err := courses.List(e.App)
	if err != nil {
		return err
	}

	return s.render(e, http.StatusOK, "courses/list.html", &courseListPage{
		Layout:  s.layout(e, v, "Courses — GolfTrack"),
		Courses: list,
	})
}

// courseArchived ports course_archived_list.
func (s *server) courseArchived(e *core.RequestEvent, v *viewer) error {
	list, err := courses.ListArchived(e.App)
	if err != nil {
		return err
	}

	return s.render(e, http.StatusOK, "courses/archived.html", &courseListPage{
		Layout:   s.layout(e, v, "Archived Courses — GolfTrack"),
		Courses:  list,
		Archived: true,
	})
}

// courseDetail ports course_detail.
func (s *server) courseDetail(e *core.RequestEvent, v *viewer) error {
	course, err := courses.Detail(e.App, e.Request.PathValue("id"))
	if err != nil {
		return err
	}

	return s.render(e, http.StatusOK, "courses/detail.html", &courseDetailPage{
		Layout: s.layout(e, v, course.Name+" — GolfTrack"),
		Course: course,
	})
}

// courseNew ports course_new's GET half. The POST half is gone: the form
// submits to `POST /api/courses/` from the page's own JavaScript, so the
// course and its holes are written in one transaction rather than by a page
// handler that would have to re-render itself on every validation error.
func (s *server) courseNew(e *core.RequestEvent, v *viewer) error {
	return s.render(e, http.StatusOK, "courses/form.html", &courseFormPage{
		Layout:     s.layout(e, v, "New Course — GolfTrack"),
		Editing:    false,
		HoleCount:  maxHoles,
		HoleRows:   holeRows(nil),
		ParOptions: parOptions,
	})
}

// courseEdit ports course_edit's GET half, pre-filling the form from the
// course's current hole set.
func (s *server) courseEdit(e *core.RequestEvent, v *viewer) error {
	course, err := courses.Detail(e.App, e.Request.PathValue("id"))
	if err != nil {
		return err
	}

	pars := make(map[int]int, len(course.Holes))
	for _, hole := range course.Holes {
		pars[hole.HoleNumber] = hole.Par
	}

	return s.render(e, http.StatusOK, "courses/form.html", &courseFormPage{
		Layout:     s.layout(e, v, "Edit Course — GolfTrack"),
		Course:     course,
		Editing:    true,
		HoleCount:  course.HoleCount,
		HoleRows:   holeRows(pars),
		ParOptions: parOptions,
	})
}

// holeRows builds all eighteen rows, defaulting the pars a course does not
// have.
func holeRows(pars map[int]int) []holeRow {
	rows := make([]holeRow, 0, maxHoles)
	for number := 1; number <= maxHoles; number++ {
		par := defaultPar
		if existing, ok := pars[number]; ok {
			par = existing
		}
		rows = append(rows, holeRow{HoleNumber: number, Par: par})
	}
	return rows
}

// -----------------------------------------------------------------------------
// rounds
// -----------------------------------------------------------------------------

type roundListPage struct {
	Layout
	InProgress *rounds.DetailOut
	Completed  []*rounds.Out
}

type playMode struct {
	Value string
	Label string
}

var playModes = []playMode{
	{collections.PlayModeFull, "Full Course"},
	{collections.PlayModeFront9, "Front 9"},
	{collections.PlayModeBack9, "Back 9"},
}

type roundNewPage struct {
	Layout
	InProgress *rounds.DetailOut
	Courses    []*courses.Out
	PlayModes  []playMode
}

type roundDetailPage struct {
	Layout
	Round *rounds.DetailOut
}

type roundPlayPage struct {
	Layout
	Round *rounds.DetailOut
	Clubs []string
}

type roundReviewPage struct {
	Layout
	Round  *rounds.DetailOut
	Totals scoring.Totals
}

// roundList ports rounds/views.round_list.
func (s *server) roundList(e *core.RequestEvent, v *viewer) error {
	inProgress, err := rounds.InProgress(e.App, v.ID())
	if err != nil {
		return err
	}
	completed, err := rounds.ListCompleted(e.App, v.ID())
	if err != nil {
		return err
	}

	return s.render(e, http.StatusOK, "rounds/list.html", &roundListPage{
		Layout:     s.layout(e, v, "Rounds — GolfTrack"),
		InProgress: inProgress,
		Completed:  completed,
	})
}

// roundNew ports round_new: pick a course, pick a play mode. The round itself
// is created by `POST /api/rounds/` from the page.
func (s *server) roundNew(e *core.RequestEvent, v *viewer) error {
	inProgress, err := rounds.InProgress(e.App, v.ID())
	if err != nil {
		return err
	}
	list, err := courses.List(e.App)
	if err != nil {
		return err
	}

	return s.render(e, http.StatusOK, "rounds/new.html", &roundNewPage{
		Layout:     s.layout(e, v, "New Round — GolfTrack"),
		InProgress: inProgress,
		Courses:    list,
		PlayModes:  playModes,
	})
}

// roundDetail ports round_detail — a completed round's scorecard.
func (s *server) roundDetail(e *core.RequestEvent, v *viewer) error {
	round, err := rounds.Detail(e.App, v.ID(), e.Request.PathValue("id"))
	if err != nil {
		return err
	}

	return s.render(e, http.StatusOK, "rounds/detail.html", &roundDetailPage{
		Layout: s.layout(e, v, "Round — "+round.Course.Name+" — GolfTrack"),
		Round:  round,
	})
}

// roundPlay ports round_play. The page bootstraps the Alpine component from a
// JSON blob of the round, the way the Django template bootstraps from
// `{{ round_data|json_script:"round-init" }}` — same shape, because both are
// RoundDetailOut.
func (s *server) roundPlay(e *core.RequestEvent, v *viewer) error {
	round, err := rounds.Detail(e.App, v.ID(), e.Request.PathValue("id"))
	if err != nil {
		return err
	}

	return s.render(e, http.StatusOK, "rounds/play.html", &roundPlayPage{
		Layout: s.layout(e, v, "Playing — "+round.Course.Name+" — GolfTrack"),
		Round:  round,
		Clubs:  rounds.DefaultClubs,
	})
}

// roundReview ports round_review: the totals as they stand right now, computed
// rather than read, because an in-progress round has not stored any yet.
func (s *server) roundReview(e *core.RequestEvent, v *viewer) error {
	round, err := rounds.Detail(e.App, v.ID(), e.Request.PathValue("id"))
	if err != nil {
		return err
	}

	holes := make([]scoring.Hole, 0, len(round.Holes))
	for _, hole := range round.Holes {
		holes = append(holes, scoring.Hole{Par: hole.Par, Strokes: hole.Strokes})
	}

	return s.render(e, http.StatusOK, "rounds/review.html", &roundReviewPage{
		Layout: s.layout(e, v, "Review Round — GolfTrack"),
		Round:  round,
		Totals: scoring.Calculate(holes),
	})
}

// -----------------------------------------------------------------------------
// accounts
// -----------------------------------------------------------------------------

type providerButton struct {
	Name        string
	DisplayName string
}

type loginPage struct {
	Layout
	Providers     []providerButton
	PasswordLogin bool
	Next          string
}

type logoutPage struct {
	Layout
	Next string
}

// login replaces django-allauth's sign-in page.
//
// Which buttons to render comes from the users collection itself rather than
// from `GET /api/collections/users/auth-methods`: the page is rendered by the
// same process that holds the collection, so asking it directly saves the
// round trip and cannot disagree with what the auth endpoint would say. The
// answer is the same one AUTH.md documents — enabled providers, and whether a
// password form belongs on the page.
func (s *server) login(e *core.RequestEvent, v *viewer) error {
	next := safeNext(e.Request.URL.Query().Get("next"))

	// Already signed in: there is nothing to do on this page.
	if v.SignedIn() {
		return e.Redirect(http.StatusFound, next)
	}

	collection, err := e.App.FindCollectionByNameOrId(collections.NameUsers)
	if err != nil {
		return err
	}

	page := &loginPage{
		Layout: s.layout(e, v, "Sign In — GolfTrack"),
		Next:   next,
	}
	if collection.OAuth2.Enabled {
		for _, provider := range collection.OAuth2.Providers {
			page.Providers = append(page.Providers, providerButton{
				Name:        provider.Name,
				DisplayName: providerDisplayName(provider.Name),
			})
		}
	}
	page.PasswordLogin = collection.PasswordAuth.Enabled

	return s.render(e, http.StatusOK, "accounts/login.html", page)
}

// logout replaces allauth's sign-out page. There is no server-side session to
// end — signing out is discarding the token, which the page's script does
// before sending the visitor home.
func (s *server) logout(e *core.RequestEvent, v *viewer) error {
	return s.render(e, http.StatusOK, "accounts/logout.html", &logoutPage{
		Layout: s.layout(e, v, "Sign Out — GolfTrack"),
	})
}

// providerDisplayName gives the two providers GolfTrack actually uses the
// names the Django sign-in page shows, and anything else a capitalised
// fallback rather than a blank button.
func providerDisplayName(name string) string {
	switch name {
	case "google":
		return "Google"
	case "microsoft":
		return "Microsoft"
	case "oidc":
		return "SSO"
	default:
		if name == "" {
			return "Provider"
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}
