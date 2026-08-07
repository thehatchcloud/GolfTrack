// The export half of round export/import (GOL-1): a player's completed rounds
// serialised to a file another GolfTrack instance can read back.
//
// The file identifies a course by name and hole count, never by record id —
// ids are instance-local, and the import contract is "the same course exists
// in the receiving instance". Hole strokes are deliberately absent from the
// format: round_holes.strokes is a cache of the hole's shot count (see
// internal/hooks/domain/shots), so exporting it would only create a second
// copy that could disagree with the shots on the way back in.
package rounds

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// ExportFormat and ExportVersion identify a GolfTrack export file. A JSON
// import is refused unless it carries both, so an arbitrary JSON document
// cannot be mistaken for rounds.
const (
	ExportFormat  = "golftrack-rounds"
	ExportVersion = 1
)

// ExportFile is the export envelope, and also the parsed form of an import —
// both formats and both directions go through this one struct, so CSV and
// JSON cannot drift into carrying different data.
type ExportFile struct {
	Format     string        `json:"format"`
	Version    int           `json:"version"`
	ExportedAt string        `json:"exportedAt"`
	Rounds     []ExportRound `json:"rounds"`
}

// ExportRound is one completed round, with the course identified portably.
type ExportRound struct {
	Course     ExportCourse `json:"course"`
	PlayMode   string       `json:"playMode"`
	StartedAt  string       `json:"startedAt"`
	FinishedAt string       `json:"finishedAt"`
	Note       string       `json:"note,omitempty"`
	Holes      []ExportHole `json:"holes"`
}

// ExportCourse names the course a round was played on. HoleCount is part of
// the identity: a 9-hole "Riverside" in the receiving instance is not the
// 18-hole "Riverside" the round was played on.
type ExportCourse struct {
	Name      string `json:"name"`
	HoleCount int    `json:"holeCount"`
}

// ExportHole carries the round's own par snapshot, not the course's current
// par — re-importing an old round must not rescore it against edits made to
// the course since (the same rule course snapshotting enforces at creation).
type ExportHole struct {
	HoleNumber int          `json:"holeNumber"`
	Par        int          `json:"par"`
	Shots      []ExportShot `json:"shots"`
}

// ExportShot is one shot, in the order it was played.
type ExportShot struct {
	ShotNumber int    `json:"shotNumber"`
	Club       string `json:"club"`
}

// BuildExport assembles every completed round the player owns, oldest first —
// unlike ListCompleted it has no limit, because a partial export defeats the
// point of the file.
func BuildExport(app core.App, userID string) (*ExportFile, error) {
	var roundRecords []*core.Record
	err := app.RecordQuery(collections.NameRounds).
		AndWhere(dbx.HashExp{
			collections.FieldUser:   userID,
			collections.FieldStatus: collections.RoundStatusCompleted,
		}).
		OrderBy(collections.FieldStartedAt + " ASC").
		All(&roundRecords)
	if err != nil {
		return nil, fmt.Errorf("list rounds for export: %w", err)
	}

	courseIDs := make([]string, 0, len(roundRecords))
	for _, round := range roundRecords {
		courseIDs = append(courseIDs, round.GetString(collections.FieldCourse))
	}
	coursesByID, err := records.CoursesByID(app, courseIDs)
	if err != nil {
		return nil, err
	}

	file := &ExportFile{
		Format:     ExportFormat,
		Version:    ExportVersion,
		ExportedAt: types.NowDateTime().String(),
		Rounds:     make([]ExportRound, 0, len(roundRecords)),
	}

	for _, round := range roundRecords {
		course, ok := coursesByID[round.GetString(collections.FieldCourse)]
		if !ok {
			return nil, fmt.Errorf("round %q references missing course %q",
				round.Id, round.GetString(collections.FieldCourse))
		}

		holes, err := exportHoles(app, round.Id)
		if err != nil {
			return nil, err
		}

		file.Rounds = append(file.Rounds, ExportRound{
			Course: ExportCourse{
				Name:      course.GetString(collections.FieldName),
				HoleCount: course.GetInt(collections.FieldHoleCount),
			},
			PlayMode:   round.GetString(collections.FieldPlayMode),
			StartedAt:  round.GetDateTime(collections.FieldStartedAt).String(),
			FinishedAt: round.GetDateTime(collections.FieldFinishedAt).String(),
			Note:       round.GetString(collections.FieldNote),
			Holes:      holes,
		})
	}

	return file, nil
}

// exportHoles loads one round's holes and their shots — two queries per round,
// not one per hole.
func exportHoles(app core.App, roundID string) ([]ExportHole, error) {
	holeRecords, err := records.RoundHoles(app, roundID)
	if err != nil {
		return nil, err
	}

	holeIDs := make([]any, 0, len(holeRecords))
	for _, hole := range holeRecords {
		holeIDs = append(holeIDs, hole.Id)
	}

	var shotRecords []*core.Record
	if len(holeIDs) > 0 {
		err = app.RecordQuery(collections.NameShots).
			AndWhere(dbx.In(collections.FieldRoundHole, holeIDs...)).
			OrderBy(collections.FieldShotNumber + " ASC").
			All(&shotRecords)
		if err != nil {
			return nil, fmt.Errorf("list shots of round %q: %w", roundID, err)
		}
	}

	shotsByHole := make(map[string][]ExportShot, len(holeRecords))
	for _, shot := range shotRecords {
		holeID := shot.GetString(collections.FieldRoundHole)
		shotsByHole[holeID] = append(shotsByHole[holeID], ExportShot{
			ShotNumber: shot.GetInt(collections.FieldShotNumber),
			Club:       shot.GetString(collections.FieldClub),
		})
	}

	holes := make([]ExportHole, 0, len(holeRecords))
	for _, hole := range holeRecords {
		holes = append(holes, ExportHole{
			HoleNumber: hole.GetInt(collections.FieldHoleNumber),
			Par:        hole.GetInt(collections.FieldPar),
			Shots:      shotsByHole[hole.Id],
		})
	}

	return holes, nil
}

// TemplateFile is a downloadable example of the import format: one 9-hole
// round that passes every check Import applies, so a player can replace its
// values with their own and upload the result. It goes through the same
// structs and serialisers as a real export, which is what keeps the template
// from drifting away from the format ParseImport actually reads.
//
// The course name is a placeholder on purpose — importing the template
// unchanged fails with the missing-course error, which is the contract the
// player's own file has to meet too.
func TemplateFile() *ExportFile {
	holes := []ExportHole{
		{HoleNumber: 1, Par: 4, Shots: []ExportShot{
			{ShotNumber: 1, Club: "Driver"},
			{ShotNumber: 2, Club: "7 Iron"},
			{ShotNumber: 3, Club: "Putter"},
		}},
		{HoleNumber: 2, Par: 3, Shots: []ExportShot{
			{ShotNumber: 1, Club: "8 Iron"},
			{ShotNumber: 2, Club: collections.ClubPenalty},
			{ShotNumber: 3, Club: "Wedge"},
			{ShotNumber: 4, Club: "Putter"},
		}},
		// A hole with no shots is allowed — in the CSV it is one row with the
		// shot columns empty.
		{HoleNumber: 3, Par: 5},
	}
	for number := 4; number <= 9; number++ {
		holes = append(holes, ExportHole{HoleNumber: number, Par: 4, Shots: []ExportShot{
			{ShotNumber: 1, Club: "Driver"},
			{ShotNumber: 2, Club: "Putter"},
		}})
	}

	return &ExportFile{
		Format:     ExportFormat,
		Version:    ExportVersion,
		ExportedAt: types.NowDateTime().String(),
		Rounds: []ExportRound{{
			Course:     ExportCourse{Name: "Replace With Your Course Name", HoleCount: 9},
			PlayMode:   collections.PlayModeFull,
			StartedAt:  "2026-05-04T09:00:00Z",
			FinishedAt: "2026-05-04T11:15:00Z",
			Note:       "An optional note about the round",
			Holes:      holes,
		}},
	}
}

// csvHeader is the CSV column contract. One row per shot; a hole a player
// recorded no shots on still gets one row, with the shot columns empty, so the
// file carries the full scorecard shape and not just the holes that were
// played.
var csvHeader = []string{
	"round", "course_name", "course_hole_count", "play_mode",
	"started_at", "finished_at", "note",
	"hole_number", "par", "shot_number", "club",
}

// WriteCSV serialises the export in the flat, spreadsheet-friendly form. The
// round-level columns repeat on every row of a round; `round` is a 1-based
// index that groups the rows back into rounds on import.
func WriteCSV(file *ExportFile, w io.Writer) error {
	writer := csv.NewWriter(w)

	if err := writer.Write(csvHeader); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	for roundIndex, round := range file.Rounds {
		prefix := []string{
			strconv.Itoa(roundIndex + 1),
			round.Course.Name,
			strconv.Itoa(round.Course.HoleCount),
			round.PlayMode,
			round.StartedAt,
			round.FinishedAt,
			round.Note,
		}

		for _, hole := range round.Holes {
			holeColumns := append(append([]string{}, prefix...),
				strconv.Itoa(hole.HoleNumber), strconv.Itoa(hole.Par))

			if len(hole.Shots) == 0 {
				if err := writer.Write(append(holeColumns, "", "")); err != nil {
					return fmt.Errorf("write csv row: %w", err)
				}
				continue
			}

			for _, shot := range hole.Shots {
				row := append(append([]string{}, holeColumns...),
					strconv.Itoa(shot.ShotNumber), shot.Club)
				if err := writer.Write(row); err != nil {
					return fmt.Errorf("write csv row: %w", err)
				}
			}
		}
	}

	writer.Flush()
	return writer.Error()
}

// ParseImport turns an uploaded file back into an ExportFile, detecting the
// format from the content — a JSON export starts with the envelope object,
// anything else is treated as CSV. Every failure is a Validation error, since
// the file is user input.
func ParseImport(data []byte) (*ExportFile, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, apierr.Validation("Import file is empty")
	}

	if strings.HasPrefix(trimmed, "{") {
		return parseJSON(trimmed)
	}
	return parseCSV(trimmed)
}

func parseJSON(data string) (*ExportFile, error) {
	file := &ExportFile{}
	if err := json.Unmarshal([]byte(data), file); err != nil {
		return nil, apierr.Validation("Import file is not valid JSON")
	}
	if file.Format != ExportFormat {
		return nil, apierr.Validation("Import file is not a GolfTrack rounds export")
	}
	if file.Version != ExportVersion {
		return nil, apierr.Validation(fmt.Sprintf(
			"Unsupported export version %d (this instance reads version %d)",
			file.Version, ExportVersion))
	}
	return file, nil
}

func parseCSV(data string) (*ExportFile, error) {
	reader := csv.NewReader(strings.NewReader(data))
	reader.FieldsPerRecord = len(csvHeader)

	header, err := reader.Read()
	if err != nil {
		return nil, apierr.Validation("Import file is not a GolfTrack rounds export")
	}
	for i, name := range csvHeader {
		if strings.TrimSpace(header[i]) != name {
			return nil, apierr.Validation("Import file is not a GolfTrack rounds export")
		}
	}

	file := &ExportFile{Format: ExportFormat, Version: ExportVersion}

	// Rows are grouped by the `round` index column. Rounds and holes are
	// keyed, not assumed contiguous, so a spreadsheet re-sort does not corrupt
	// the import; shot order within a hole is still row order, validated
	// against shot_number later by Import. Holes are allocated individually
	// and only assembled into their round's slice at the end, so a growing
	// slice never invalidates the pointer shots are appended through.
	roundByIndex := map[string]*ExportRound{}
	holeByKey := map[string]*ExportHole{}
	holeOrder := map[string][]string{}
	var order []string

	for line := 2; ; line++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, apierr.Validation(fmt.Sprintf("Import file line %d is not valid CSV", line))
		}

		roundKey := strings.TrimSpace(row[0])
		if roundKey == "" {
			return nil, apierr.Validation(fmt.Sprintf("Import file line %d is missing its round number", line))
		}

		round, ok := roundByIndex[roundKey]
		if !ok {
			holeCount, err := csvInt(row[2])
			if err != nil {
				return nil, apierr.Validation(fmt.Sprintf("Import file line %d has an invalid course_hole_count", line))
			}
			round = &ExportRound{
				Course:     ExportCourse{Name: row[1], HoleCount: holeCount},
				PlayMode:   strings.TrimSpace(row[3]),
				StartedAt:  strings.TrimSpace(row[4]),
				FinishedAt: strings.TrimSpace(row[5]),
				Note:       row[6],
			}
			roundByIndex[roundKey] = round
			order = append(order, roundKey)
		}

		holeNumber, err := csvInt(row[7])
		if err != nil {
			return nil, apierr.Validation(fmt.Sprintf("Import file line %d has an invalid hole_number", line))
		}
		par, err := csvInt(row[8])
		if err != nil {
			return nil, apierr.Validation(fmt.Sprintf("Import file line %d has an invalid par", line))
		}

		holeKey := roundKey + "/" + strconv.Itoa(holeNumber)
		hole, ok := holeByKey[holeKey]
		if !ok {
			hole = &ExportHole{HoleNumber: holeNumber, Par: par}
			holeByKey[holeKey] = hole
			holeOrder[roundKey] = append(holeOrder[roundKey], holeKey)
		}

		if strings.TrimSpace(row[9]) == "" {
			continue // a hole with no shots carries one shotless row
		}
		shotNumber, err := csvInt(row[9])
		if err != nil {
			return nil, apierr.Validation(fmt.Sprintf("Import file line %d has an invalid shot_number", line))
		}
		hole.Shots = append(hole.Shots, ExportShot{ShotNumber: shotNumber, Club: row[10]})
	}

	for _, key := range order {
		round := roundByIndex[key]
		for _, holeKey := range holeOrder[key] {
			round.Holes = append(round.Holes, *holeByKey[holeKey])
		}
		file.Rounds = append(file.Rounds, *round)
	}
	return file, nil
}

func csvInt(value string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(value))
}
