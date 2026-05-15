-- CreateIndex: partial unique index enforcing at most one in-progress round at the DB level
CREATE UNIQUE INDEX "rounds_one_in_progress" ON "rounds" ("status") WHERE status = 'in_progress';
