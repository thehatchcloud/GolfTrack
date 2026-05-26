-- AlterTable: link rounds to the user who created them
ALTER TABLE "rounds" ADD COLUMN "user_id" TEXT;

-- CreateIndex
CREATE INDEX "rounds_user_id_idx" ON "rounds"("user_id");
