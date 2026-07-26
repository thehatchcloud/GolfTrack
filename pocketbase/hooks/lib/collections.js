/**
 * Collection names and ids, mirroring `pocketbase/pb_schema.json`.
 *
 * Hook code should reference collections through these constants rather than
 * inline strings so a rename is a single-file change.
 */
const NAMES = {
    USERS: "users",
    COURSES: "courses",
    COURSE_HOLES: "course_holes",
    ROUNDS: "rounds",
    ROUND_HOLES: "round_holes",
    SHOTS: "shots",
};

const IDS = {
    USERS: "_pb_users_auth_",
    COURSES: "golftrack_courses",
    COURSE_HOLES: "golftrack_course_holes",
    ROUNDS: "golftrack_rounds",
    ROUND_HOLES: "golftrack_round_holes",
    SHOTS: "golftrack_shots",
};

/** Enum values, kept in sync with the `select` fields in pb_schema.json. */
const ROUND_STATUS = {
    IN_PROGRESS: "in_progress",
    COMPLETED: "completed",
};

const PLAY_MODE = {
    FULL: "full",
    FRONT9: "front9",
    BACK9: "back9",
};

const USER_ROLE = {
    USER: "USER",
    ADMIN: "ADMIN",
};

module.exports = { NAMES, IDS, ROUND_STATUS, PLAY_MODE, USER_ROLE };
