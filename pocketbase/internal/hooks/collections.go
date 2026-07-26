package hooks

// Collection names and ids, mirroring pocketbase/pb_schema.json. Hook code
// references collections through these constants rather than inline strings so
// a rename is a single-file change.
const (
	NameUsers       = "users"
	NameCourses     = "courses"
	NameCourseHoles = "course_holes"
	NameRounds      = "rounds"
	NameRoundHoles  = "round_holes"
	NameShots       = "shots"
)

const (
	IDUsers       = "_pb_users_auth_"
	IDCourses     = "golftrack_courses"
	IDCourseHoles = "golftrack_course_holes"
	IDRounds      = "golftrack_rounds"
	IDRoundHoles  = "golftrack_round_holes"
	IDShots       = "golftrack_shots"
)

// CollectionNames lists the six GolfTrack collections in dependency order.
var CollectionNames = []string{
	NameUsers,
	NameCourses,
	NameCourseHoles,
	NameRounds,
	NameRoundHoles,
	NameShots,
}

// Field names referenced by hook code. Only the fields hooks actually touch
// are named here; the full field list lives in pb_schema.json.
const (
	FieldRole = "role"
)

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
