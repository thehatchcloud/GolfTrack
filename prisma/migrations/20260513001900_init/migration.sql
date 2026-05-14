-- CreateTable
CREATE TABLE "courses" (
    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    "name" TEXT NOT NULL,
    "hole_count" INTEGER NOT NULL,
    "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" DATETIME NOT NULL
);

-- CreateTable
CREATE TABLE "course_holes" (
    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    "course_id" INTEGER NOT NULL,
    "hole_number" INTEGER NOT NULL,
    "par" INTEGER NOT NULL,
    CONSTRAINT "course_holes_course_id_fkey" FOREIGN KEY ("course_id") REFERENCES "courses" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);

-- CreateTable
CREATE TABLE "rounds" (
    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    "course_id" INTEGER NOT NULL,
    "status" TEXT NOT NULL,
    "started_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "finished_at" DATETIME,
    "note" TEXT,
    "current_hole" INTEGER NOT NULL DEFAULT 1,
    "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" DATETIME NOT NULL,
    CONSTRAINT "rounds_course_id_fkey" FOREIGN KEY ("course_id") REFERENCES "courses" ("id") ON DELETE RESTRICT ON UPDATE CASCADE
);

-- CreateTable
CREATE TABLE "round_holes" (
    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    "round_id" INTEGER NOT NULL,
    "hole_number" INTEGER NOT NULL,
    "par" INTEGER NOT NULL,
    "strokes" INTEGER NOT NULL DEFAULT 0,
    "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" DATETIME NOT NULL,
    CONSTRAINT "round_holes_round_id_fkey" FOREIGN KEY ("round_id") REFERENCES "rounds" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);

-- CreateTable
CREATE TABLE "shots" (
    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    "round_hole_id" INTEGER NOT NULL,
    "shot_number" INTEGER NOT NULL,
    "club" TEXT NOT NULL,
    "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "shots_round_hole_id_fkey" FOREIGN KEY ("round_hole_id") REFERENCES "round_holes" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);

-- CreateIndex
CREATE UNIQUE INDEX "course_holes_course_id_hole_number_key" ON "course_holes"("course_id", "hole_number");

-- CreateIndex
CREATE UNIQUE INDEX "round_holes_round_id_hole_number_key" ON "round_holes"("round_id", "hole_number");

-- CreateIndex
CREATE UNIQUE INDEX "shots_round_hole_id_shot_number_key" ON "shots"("round_hole_id", "shot_number");
