/*
  Phase 3: Add user_id to Round model for per-user data scoping.

  This migration:
  1. Deletes all existing rounds (and their cascading round_holes/shots)
  2. Adds user_id as a required FK to the User model
  3. Creates indexes for efficient querying by userId
*/

-- Delete all existing rounds (cascades to round_holes and shots via foreign keys)
DELETE FROM "rounds";

-- RedefineTables
PRAGMA defer_foreign_keys=ON;
PRAGMA foreign_keys=OFF;
CREATE TABLE "new_rounds" (
    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    "user_id" TEXT NOT NULL,
    "course_id" INTEGER NOT NULL,
    "status" TEXT NOT NULL,
    "play_mode" TEXT NOT NULL DEFAULT 'full',
    "started_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "finished_at" DATETIME,
    "note" TEXT,
    "current_hole" INTEGER NOT NULL DEFAULT 1,
    "total_strokes" INTEGER,
    "total_par" INTEGER,
    "relative_to_par" INTEGER,
    "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" DATETIME NOT NULL,
    CONSTRAINT "rounds_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "rounds_course_id_fkey" FOREIGN KEY ("course_id") REFERENCES "courses" ("id") ON DELETE RESTRICT ON UPDATE CASCADE
);
DROP TABLE "rounds";
ALTER TABLE "new_rounds" RENAME TO "rounds";
CREATE INDEX "rounds_user_id_idx" ON "rounds"("user_id");
CREATE INDEX "rounds_status_idx" ON "rounds"("status");
CREATE INDEX "rounds_course_id_idx" ON "rounds"("course_id");
PRAGMA foreign_keys=ON;
PRAGMA defer_foreign_keys=OFF;
