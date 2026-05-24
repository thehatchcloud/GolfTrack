-- AlterTable: add pre-computed summary columns and play_mode to rounds
ALTER TABLE "rounds" ADD COLUMN "play_mode" TEXT NOT NULL DEFAULT 'full';
ALTER TABLE "rounds" ADD COLUMN "total_strokes" INTEGER;
ALTER TABLE "rounds" ADD COLUMN "total_par" INTEGER;
ALTER TABLE "rounds" ADD COLUMN "relative_to_par" INTEGER;

-- Backfill totals for existing completed rounds
UPDATE "rounds"
SET
  "total_strokes" = (SELECT COALESCE(SUM("strokes"), 0) FROM "round_holes" WHERE "round_id" = "rounds"."id"),
  "total_par"     = (SELECT COALESCE(SUM("par"),     0) FROM "round_holes" WHERE "round_id" = "rounds"."id")
WHERE "status" = 'completed';

UPDATE "rounds"
SET "relative_to_par" = "total_strokes" - "total_par"
WHERE "status" = 'completed';

-- Backfill play_mode from hole numbers for existing rounds
UPDATE "rounds"
SET "play_mode" = CASE
  WHEN (SELECT "hole_count" FROM "courses" WHERE "id" = "rounds"."course_id") = 18
    AND (SELECT COUNT(*) FROM "round_holes" WHERE "round_id" = "rounds"."id") = 9
    AND (SELECT MIN("hole_number") FROM "round_holes" WHERE "round_id" = "rounds"."id") = 1
  THEN 'front9'
  WHEN (SELECT "hole_count" FROM "courses" WHERE "id" = "rounds"."course_id") = 18
    AND (SELECT COUNT(*) FROM "round_holes" WHERE "round_id" = "rounds"."id") = 9
    AND (SELECT MIN("hole_number") FROM "round_holes" WHERE "round_id" = "rounds"."id") = 10
  THEN 'back9'
  ELSE 'full'
END;
