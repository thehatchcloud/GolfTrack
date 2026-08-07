package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/rounds"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// Round export/import suite (GOL-1): a player's completed rounds serialised to
// JSON or CSV and rebuilt in another instance, where "another instance" is
// literally a second newTestApp — separate database, separate record ids, only
// the course name in common. That is the scenario the issue describes, so the
// round-trip tests cross two apps rather than re-importing into the source.

// seedCompletedRound builds a completed round the way one is really played:
// created in progress, shots inserted through the hooks (so the stroke cache
// is theirs, not the fixture's), then completed. clubsByHole[i] holds hole
// i+1's shots; every hole gets the given par.
func seedCompletedRound(t testing.TB, app core.App, user, course *core.Record, playMode string, startedAt, finishedAt time.Time, note string, par int, clubsByHole [][]string) *core.Record {
	t.Helper()

	round := saveAs(t, app, newRecord(t, app, collections.NameRounds, "", map[string]any{
		"user":         user.Id,
		"course":       course.Id,
		"status":       collections.RoundStatusInProgress,
		"play_mode":    playMode,
		"started_at":   startedAt,
		"current_hole": 1,
	}))

	totalStrokes := 0
	for i, clubs := range clubsByHole {
		hole := createRoundHole(t, app, "", round, i+1, par)
		for n, club := range clubs {
			createShot(t, app, "", hole, n+1, club)
			totalStrokes++
		}
	}

	totalPar := par * len(clubsByHole)
	round.Set(collections.FieldStatus, collections.RoundStatusCompleted)
	round.Set(collections.FieldFinishedAt, finishedAt)
	round.Set(collections.FieldNote, note)
	round.Set(collections.FieldTotalStrokes, totalStrokes)
	round.Set(collections.FieldTotalPar, totalPar)
	round.Set(collections.FieldRelativeToPar, totalStrokes-totalPar)
	return saveAs(t, app, round)
}

// nineHoleClubs is a 9-hole scorecard: hole 1 with three shots (one a
// penalty), hole 2 never played, the rest one putt each.
func nineHoleClubs() [][]string {
	clubs := [][]string{
		{"Driver", collections.ClubPenalty, "Wedge"},
		{},
	}
	for i := 2; i < 9; i++ {
		clubs = append(clubs, []string{"Putter"})
	}
	return clubs
}

var (
	exportStartedAt  = time.Date(2026, 7, 14, 13, 30, 0, 0, time.UTC)
	exportFinishedAt = time.Date(2026, 7, 14, 15, 45, 0, 0, time.UTC)
)

// -----------------------------------------------------------------------------
// export
// -----------------------------------------------------------------------------

func TestExportBuildsTheFullHistoryAndNothingElse(t *testing.T) {
	app := newTestApp(t)
	owner := createUser(t, app, idOwner, "owner@golftrack.test", collections.UserRoleUser)
	other := createUser(t, app, idOther, "other@golftrack.test", collections.UserRoleUser)
	course := seedCourse(t, app, idCourse, "Nine Acres", 9, 4)

	seedCompletedRound(t, app, owner, course, collections.PlayModeFull,
		exportStartedAt, exportFinishedAt, "windy", 4, nineHoleClubs())

	// Neither the owner's in-progress round nor another player's completed
	// round belongs in the file.
	createRound(t, app, "", owner, course, collections.RoundStatusInProgress)
	seedCompletedRound(t, app, other, course, collections.PlayModeFull,
		exportStartedAt.Add(time.Hour), exportFinishedAt.Add(time.Hour), "", 4, nineHoleClubs())

	file, err := rounds.BuildExport(app, owner.Id)
	if err != nil {
		t.Fatalf("BuildExport: %v", err)
	}

	if file.Format != rounds.ExportFormat || file.Version != rounds.ExportVersion {
		t.Errorf("envelope = %q v%d, want %q v%d", file.Format, file.Version, rounds.ExportFormat, rounds.ExportVersion)
	}
	if len(file.Rounds) != 1 {
		t.Fatalf("exported %d rounds, want 1", len(file.Rounds))
	}

	round := file.Rounds[0]
	if round.Course.Name != "Nine Acres" || round.Course.HoleCount != 9 {
		t.Errorf("course = %+v, want Nine Acres/9", round.Course)
	}
	if round.PlayMode != collections.PlayModeFull || round.Note != "windy" {
		t.Errorf("round = playMode %q note %q", round.PlayMode, round.Note)
	}
	if len(round.Holes) != 9 {
		t.Fatalf("exported %d holes, want 9", len(round.Holes))
	}
	if got := round.Holes[0].Shots; len(got) != 3 || got[1].Club != collections.ClubPenalty || got[2].ShotNumber != 3 {
		t.Errorf("hole 1 shots = %+v, want Driver/Penalty/Wedge numbered 1..3", got)
	}
	if len(round.Holes[1].Shots) != 0 {
		t.Errorf("hole 2 shots = %+v, want none", round.Holes[1].Shots)
	}
}

// -----------------------------------------------------------------------------
// round trips
// -----------------------------------------------------------------------------

// importDestination is "another instance of GolfTrack": a second app whose
// course shares nothing with the source's but its name and hole count.
func importDestination(t testing.TB, courseName string, holeCount int) (*tests.TestApp, *core.Record) {
	t.Helper()

	app := newTestApp(t)
	user := createUser(t, app, "", "importer@golftrack.test", collections.UserRoleUser)
	if courseName != "" {
		seedCourse(t, app, "", courseName, holeCount, 3)
	}
	return app, user
}

func exportedNineHoleFile(t testing.TB) *rounds.ExportFile {
	t.Helper()

	app := newTestApp(t)
	owner := createUser(t, app, idOwner, "owner@golftrack.test", collections.UserRoleUser)
	course := seedCourse(t, app, idCourse, "Nine Acres", 9, 4)
	seedCompletedRound(t, app, owner, course, collections.PlayModeFull,
		exportStartedAt, exportFinishedAt, "windy", 4, nineHoleClubs())

	file, err := rounds.BuildExport(app, owner.Id)
	if err != nil {
		t.Fatalf("BuildExport: %v", err)
	}
	return file
}

func TestJSONRoundTripAcrossInstances(t *testing.T) {
	file := exportedNineHoleFile(t)

	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	parsed, err := rounds.ParseImport(raw)
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}

	destination, importer := importDestination(t, "Nine Acres", 9)
	result, err := rounds.Import(destination, importer.Id, parsed)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Imported != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v, want 1 imported, 0 skipped", result)
	}

	imported, err := destination.FindAllRecords(collections.NameRounds)
	if err != nil || len(imported) != 1 {
		t.Fatalf("destination rounds = %d (%v), want 1", len(imported), err)
	}
	round := imported[0]

	if got := round.GetString(collections.FieldStatus); got != collections.RoundStatusCompleted {
		t.Errorf("status = %q, want completed", got)
	}
	if got := round.GetString(collections.FieldUser); got != importer.Id {
		t.Errorf("user = %q, want the importer", got)
	}
	if got := round.GetString(collections.FieldNote); got != "windy" {
		t.Errorf("note = %q, want windy", got)
	}
	if got := round.GetDateTime(collections.FieldStartedAt).Time(); !got.Equal(exportStartedAt) {
		t.Errorf("started_at = %v, want %v", got, exportStartedAt)
	}
	if got := round.GetDateTime(collections.FieldFinishedAt).Time(); !got.Equal(exportFinishedAt) {
		t.Errorf("finished_at = %v, want %v", got, exportFinishedAt)
	}

	// 3 shots + 7 putts over 9 holes of par 4: the totals must be recomputed
	// from the rebuilt shots, and the pars must be the file's snapshot (4),
	// not the destination course's own par (3).
	if got := round.GetInt(collections.FieldTotalStrokes); got != 10 {
		t.Errorf("total_strokes = %d, want 10", got)
	}
	if got := round.GetInt(collections.FieldTotalPar); got != 36 {
		t.Errorf("total_par = %d, want 36", got)
	}
	if got := round.GetInt(collections.FieldRelativeToPar); got != -26 {
		t.Errorf("relative_to_par = %d, want -26", got)
	}

	holes, err := records.RoundHoles(destination, round.Id)
	if err != nil || len(holes) != 9 {
		t.Fatalf("imported holes = %d (%v), want 9", len(holes), err)
	}
	if got := holes[0].GetInt(collections.FieldPar); got != 4 {
		t.Errorf("hole 1 par = %d, want the snapshot's 4", got)
	}
	strokes, shotNumbers, clubs := holeState(t, destination, holes[0].Id)
	if strokes != 3 || !equalInts(shotNumbers, []int{1, 2, 3}) {
		t.Errorf("hole 1 = %d strokes, shots %v; want the cache at 3 over shots 1..3", strokes, shotNumbers)
	}
	if len(clubs) != 3 || clubs[1] != collections.ClubPenalty {
		t.Errorf("hole 1 clubs = %v, want the penalty preserved", clubs)
	}
	if strokes, _, _ := holeState(t, destination, holes[1].Id); strokes != 0 {
		t.Errorf("hole 2 strokes = %d, want 0", strokes)
	}
}

func TestCSVRoundTripMatchesTheJSONExport(t *testing.T) {
	file := exportedNineHoleFile(t)

	var buf bytes.Buffer
	if err := rounds.WriteCSV(file, &buf); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	parsed, err := rounds.ParseImport(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseImport(csv): %v", err)
	}
	if !reflect.DeepEqual(parsed.Rounds, file.Rounds) {
		t.Fatalf("csv round-trip drifted:\n got %+v\nwant %+v", parsed.Rounds, file.Rounds)
	}

	destination, importer := importDestination(t, "Nine Acres", 9)
	result, err := rounds.Import(destination, importer.Id, parsed)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Imported != 1 {
		t.Fatalf("result = %+v, want 1 imported", result)
	}
}

func TestImportingTheSameFileTwiceSkips(t *testing.T) {
	file := exportedNineHoleFile(t)
	destination, importer := importDestination(t, "Nine Acres", 9)

	if _, err := rounds.Import(destination, importer.Id, file); err != nil {
		t.Fatalf("first import: %v", err)
	}
	result, err := rounds.Import(destination, importer.Id, file)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if result.Imported != 0 || result.Skipped != 1 {
		t.Errorf("second import = %+v, want 0 imported, 1 skipped", result)
	}

	total, err := destination.FindAllRecords(collections.NameRounds)
	if err != nil || len(total) != 1 {
		t.Errorf("destination rounds = %d (%v), want 1", len(total), err)
	}
}

// TestTemplateFileIsAValidImport pins the template download to the contract it
// exists for: a player can fill it in and upload it. If a format or validation
// change breaks that, this is the test that says so.
func TestTemplateFileIsAValidImport(t *testing.T) {
	file := rounds.TemplateFile()

	// The CSV rendering parses back to the same rounds, so both template
	// formats teach the same file.
	var buf bytes.Buffer
	if err := rounds.WriteCSV(file, &buf); err != nil {
		t.Fatalf("WriteCSV(template): %v", err)
	}
	parsed, err := rounds.ParseImport(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseImport(csv template): %v", err)
	}
	if !reflect.DeepEqual(parsed.Rounds, file.Rounds) {
		t.Fatalf("csv template drifted:\n got %+v\nwant %+v", parsed.Rounds, file.Rounds)
	}

	// Unchanged, the template must fail with the missing-course error — its
	// course name is a placeholder, not a real course.
	destination, importer := importDestination(t, "", 0)
	_, err = rounds.Import(destination, importer.Id, rounds.TemplateFile())
	wantAPIError(t, err, http.StatusBadRequest,
		`Cannot import: course "Replace With Your Course Name" does not exist in this instance`)

	// Once a matching course exists, the template imports as-is: every other
	// validation rule (hole coverage, shot numbering, pars, dates) holds.
	destination, importer = importDestination(t, "Replace With Your Course Name", 9)
	result, err := rounds.Import(destination, importer.Id, rounds.TemplateFile())
	if err != nil {
		t.Fatalf("Import(template): %v", err)
	}
	if result.Imported != 1 {
		t.Fatalf("result = %+v, want 1 imported", result)
	}
}

// -----------------------------------------------------------------------------
// import rejections
// -----------------------------------------------------------------------------

func TestImportRequiresTheCourseToExist(t *testing.T) {
	file := exportedNineHoleFile(t)

	destination, importer := importDestination(t, "", 0)
	_, err := rounds.Import(destination, importer.Id, file)
	wantAPIError(t, err, http.StatusBadRequest,
		`Cannot import: course "Nine Acres" does not exist in this instance`)
}

func TestImportRequiresTheCourseHoleCountToMatch(t *testing.T) {
	file := exportedNineHoleFile(t)

	destination, importer := importDestination(t, "Nine Acres", 18)
	_, err := rounds.Import(destination, importer.Id, file)
	wantAPIError(t, err, http.StatusBadRequest,
		`Cannot import: course "Nine Acres" exists but does not have 9 holes`)
}

func TestImportRefusesWhileARoundIsInProgress(t *testing.T) {
	file := exportedNineHoleFile(t)

	destination, importer := importDestination(t, "Nine Acres", 9)
	course, err := destination.FindFirstRecordByData(collections.NameCourses, collections.FieldName, "Nine Acres")
	if err != nil {
		t.Fatalf("find destination course: %v", err)
	}
	createRound(t, destination, "", importer, course, collections.RoundStatusInProgress)

	_, err = rounds.Import(destination, importer.Id, file)
	wantAPIError(t, err, http.StatusConflict, "Finish or cancel your in-progress round before importing")
}

func TestParseImportRejectsForeignFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want string
	}{
		{"empty", "   ", "Import file is empty"},
		{"arbitrary json", `{"rounds": []}`, "Import file is not a GolfTrack rounds export"},
		{"broken json", `{"format":`, "Import file is not valid JSON"},
		{"future version", `{"format":"golftrack-rounds","version":99}`,
			"Unsupported export version 99 (this instance reads version 1)"},
		{"arbitrary csv", "a,b,c\n1,2,3", "Import file is not a GolfTrack rounds export"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rounds.ParseImport([]byte(tc.data))
			wantAPIError(t, err, http.StatusBadRequest, tc.want)
		})
	}
}

func TestImportValidatesTheRoundsBeforeWritingAnything(t *testing.T) {
	base := func() *rounds.ExportFile {
		file := exportedNineHoleFile(t)
		return file
	}

	for _, tc := range []struct {
		name   string
		mutate func(*rounds.ExportFile)
		want   string
	}{
		{"no rounds", func(f *rounds.ExportFile) { f.Rounds = nil },
			"Import file contains no rounds"},
		{"bad play mode", func(f *rounds.ExportFile) { f.Rounds[0].PlayMode = "back6" },
			`Round 1 has an invalid play mode: "back6"`},
		{"back9 on a 9-hole course", func(f *rounds.ExportFile) { f.Rounds[0].PlayMode = collections.PlayModeBack9 },
			"Round 1 plays front 9 or back 9 on a 9-hole course"},
		{"bad par", func(f *rounds.ExportFile) { f.Rounds[0].Holes[3].Par = 2 },
			"Round 1 hole 4 has an invalid par: 2"},
		{"duplicate hole", func(f *rounds.ExportFile) { f.Rounds[0].Holes[1].HoleNumber = 1 },
			"Round 1 lists hole 1 twice"},
		{"missing hole", func(f *rounds.ExportFile) { f.Rounds[0].Holes = f.Rounds[0].Holes[:8] },
			"Round 1 does not cover every hole its play mode requires"},
		{"shots out of order", func(f *rounds.ExportFile) { f.Rounds[0].Holes[0].Shots[1].ShotNumber = 9 },
			"Round 1 hole 1's shots are not numbered 1..3 in order"},
		{"blank club", func(f *rounds.ExportFile) { f.Rounds[0].Holes[0].Shots[0].Club = "  " },
			"Round 1 hole 1 shot 1 has an invalid club"},
		{"finish before start", func(f *rounds.ExportFile) {
			f.Rounds[0].FinishedAt = exportStartedAt.Add(-time.Hour).Format(time.RFC3339)
		}, "Round 1 finishes before it starts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := base()
			tc.mutate(file)

			destination, importer := importDestination(t, "Nine Acres", 9)
			_, err := rounds.Import(destination, importer.Id, file)
			wantAPIError(t, err, http.StatusBadRequest, tc.want)

			leftover, err := destination.FindAllRecords(collections.NameRounds)
			if err != nil || len(leftover) != 0 {
				t.Errorf("rejected import left %d rounds behind (%v)", len(leftover), err)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// the routes
// -----------------------------------------------------------------------------

func TestExportImportRoutesRequireAuth(t *testing.T) {
	for _, tc := range []struct {
		method string
		url    string
	}{
		{http.MethodGet, "/api/rounds/export"},
		{http.MethodPost, "/api/rounds/import"},
		{http.MethodGet, "/api/rounds/import/template"},
	} {
		runPlay(t, nil, tests.ApiScenario{
			Name:            "anonymous " + tc.method + " " + tc.url,
			Method:          tc.method,
			URL:             tc.url,
			ExpectedStatus:  401,
			ExpectedContent: []string{`"status":401`},
		})
	}
}

func TestExportRoute(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "json export carries the envelope and the completed round",
		Method:         http.MethodGet,
		URL:            "/api/rounds/export",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"format":"golftrack-rounds"`,
			`"version":1`,
			`"name":"Play Links"`,
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if got := res.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, ".json") {
				t.Errorf("Content-Disposition = %q, want a .json attachment", got)
			}
		},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "csv export carries the header row",
		Method:          http.MethodGet,
		URL:             "/api/rounds/export?format=csv",
		ExpectedStatus:  200,
		ExpectedContent: []string{"round,course_name,course_hole_count,play_mode"},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "unknown format is rejected",
		Method:          http.MethodGet,
		URL:             "/api/rounds/export?format=xml",
		ExpectedStatus:  400,
		ExpectedContent: []string{`"error":"Invalid export format: xml"`},
	})
}

func TestImportTemplateRoute(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "json template carries the envelope and the placeholder course",
		Method:         http.MethodGet,
		URL:            "/api/rounds/import/template",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"format":"golftrack-rounds"`,
			`"version":1`,
			`"name":"Replace With Your Course Name"`,
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if got := res.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "golftrack-import-template.json") {
				t.Errorf("Content-Disposition = %q, want the .json template attachment", got)
			}
		},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "csv template carries the header row",
		Method:          http.MethodGet,
		URL:             "/api/rounds/import/template?format=csv",
		ExpectedStatus:  200,
		ExpectedContent: []string{"round,course_name,course_hole_count,play_mode"},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "unknown template format is rejected",
		Method:          http.MethodGet,
		URL:             "/api/rounds/import/template?format=xml",
		ExpectedStatus:  400,
		ExpectedContent: []string{`"error":"Invalid template format: xml"`},
	})
}

func TestImportRoute(t *testing.T) {
	// A file the play fixture can absorb: one full round on its 18-hole
	// course. `other` imports it — the fixture's owner is mid-round, which the
	// import refuses by design.
	file := &rounds.ExportFile{Format: rounds.ExportFormat, Version: rounds.ExportVersion}
	round := rounds.ExportRound{
		Course:     rounds.ExportCourse{Name: "Play Links", HoleCount: 18},
		PlayMode:   collections.PlayModeFull,
		StartedAt:  exportStartedAt.Format(time.RFC3339),
		FinishedAt: exportFinishedAt.Format(time.RFC3339),
	}
	for number := 1; number <= 18; number++ {
		round.Holes = append(round.Holes, rounds.ExportHole{
			HoleNumber: number,
			Par:        4,
			Shots:      []rounds.ExportShot{{ShotNumber: 1, Club: "Driver"}},
		})
	}
	file.Rounds = []rounds.ExportRound{round}

	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal import body: %v", err)
	}

	runPlay(t, playOther, tests.ApiScenario{
		Name:            "import creates the round for the caller",
		Method:          http.MethodPost,
		URL:             "/api/rounds/import",
		Body:            bytes.NewReader(raw),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"imported":1`, `"skipped":0`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			imported, err := app.FindAllRecords(collections.NameRounds, dbx.HashExp{collections.FieldUser: idOther})
			if err != nil || len(imported) != 1 {
				t.Fatalf("imported rounds of other = %d (%v), want 1", len(imported), err)
			}
			if got := imported[0].GetInt(collections.FieldTotalStrokes); got != 18 {
				t.Errorf("total_strokes = %d, want 18", got)
			}
		},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "import refuses while the caller is mid-round",
		Method:          http.MethodPost,
		URL:             "/api/rounds/import",
		Body:            bytes.NewReader(raw),
		ExpectedStatus:  409,
		ExpectedContent: []string{`"error":"Finish or cancel your in-progress round before importing"`},
	})
}
