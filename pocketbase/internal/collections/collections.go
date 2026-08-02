// Package collections is the vocabulary of the GolfTrack schema: collection
// names and ids, the field names hook code touches, and the values of the
// select fields — all mirroring pocketbase/pb_schema.json.
//
// It is deliberately a leaf package. Both internal/hooks (the registrar) and
// every domain package under internal/hooks/domain reference these constants,
// and the registrar imports the domain packages, so the constants cannot live
// in the registrar without an import cycle. Keeping them here also preserves
// the original property: a rename is a single-file change, never a grep for a
// string literal.
package collections

// Collection names.
const (
	NameUsers       = "users"
	NameCourses     = "courses"
	NameCourseHoles = "course_holes"
	NameRounds      = "rounds"
	NameRoundHoles  = "round_holes"
	NameShots       = "shots"
)

// Collection ids.
const (
	IDUsers       = "_pb_users_auth_"
	IDCourses     = "golftrack_courses"
	IDCourseHoles = "golftrack_course_holes"
	IDRounds      = "golftrack_rounds"
	IDRoundHoles  = "golftrack_round_holes"
	IDShots       = "golftrack_shots"
)

// Names lists the six GolfTrack collections in dependency order.
var Names = []string{
	NameUsers,
	NameCourses,
	NameCourseHoles,
	NameRounds,
	NameRoundHoles,
	NameShots,
}

// Field names referenced by hook code. Only the fields hooks actually touch are
// named here; the full field list lives in pb_schema.json.
const (
	// users
	FieldRole = "role"

	// courses
	FieldName       = "name"
	FieldHoleCount  = "hole_count"
	FieldTimeZone   = "time_zone"
	FieldIsArchived = "is_archived"

	// timestamps, shared by every collection that has them
	FieldCreatedAt = "created_at"
	FieldUpdatedAt = "updated_at"

	// course_holes, round_holes
	FieldCourse     = "course"
	FieldRound      = "round"
	FieldHoleNumber = "hole_number"
	FieldPar        = "par"
	FieldStrokes    = "strokes"

	// rounds
	FieldUser          = "user"
	FieldStatus        = "status"
	FieldPlayMode      = "play_mode"
	FieldStartedAt     = "started_at"
	FieldFinishedAt    = "finished_at"
	FieldNote          = "note"
	FieldCurrentHole   = "current_hole"
	FieldTotalStrokes  = "total_strokes"
	FieldTotalPar      = "total_par"
	FieldRelativeToPar = "relative_to_par"

	// shots
	FieldRoundHole  = "round_hole"
	FieldShotNumber = "shot_number"
	FieldClub       = "club"
)

// FieldCourseTotalPar is the derived course total, computed per response rather
// than stored (Django exposes it as the `Course.total_par` property). It is not
// a column in pb_schema.json — see internal/hooks/domain/courses.
const FieldCourseTotalPar = "total_par"

// Enum values, kept in sync with the `select` fields in pb_schema.json.
const (
	RoundStatusInProgress = "in_progress"
	RoundStatusCompleted  = "completed"

	PlayModeFull   = "full"
	PlayModeFront9 = "front9"
	PlayModeBack9  = "back9"

	UserRoleUser  = "USER"
	UserRoleAdmin = "ADMIN"
)

// HoleCounts are the two course sizes GolfTrack supports. The schema bounds
// hole_count to 9–18 because PocketBase number fields have no value set; the
// exact rule (Django's `hole_count: Literal[9, 18]`) is enforced by a hook.
var HoleCounts = []int{9, 18}
