import { execSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

const TEST_DB_FILENAME = 'test.db'
const TEST_DB_PATH = path.join(process.cwd(), 'prisma', TEST_DB_FILENAME)
const TEST_DB_JOURNAL_PATH = `${TEST_DB_PATH}-journal`

// Use dev.db for E2E tests so the dev server can see the test data
const TEST_DATABASE_URL = process.env.TEST_USE_DEV_DB === 'true'
  ? `file:./prisma/dev.db`
  : `file:./prisma/${TEST_DB_FILENAME}`

if (process.env.TEST_USE_DEV_DB !== 'true') {
  process.env.DATABASE_URL = TEST_DATABASE_URL
}

function removeTestDatabaseFiles() {
  // Don't remove dev.db, only remove test.db
  if (process.env.TEST_USE_DEV_DB !== 'true') {
    for (const filePath of [TEST_DB_PATH, TEST_DB_JOURNAL_PATH]) {
      if (fs.existsSync(filePath)) {
        fs.rmSync(filePath, { force: true })
      }
    }
  }
}

export async function setupTestContext() {
  // For E2E tests using dev.db, don't reset the entire database
  // Instead, we'll clean up specific test data in resetDatabase()
  if (process.env.TEST_USE_DEV_DB !== 'true') {
    removeTestDatabaseFiles()
  }

  execSync('npx prisma db push --skip-generate', {
    cwd: process.cwd(),
    env: {
      ...process.env,
      DATABASE_URL: TEST_DATABASE_URL,
    },
    stdio: 'ignore',
  })

  const [{ db }, courses, rounds] = await Promise.all([
    import('../../lib/db'),
    import('../../lib/courses'),
    import('../../lib/rounds'),
  ])

  await db.$executeRawUnsafe(
    `CREATE UNIQUE INDEX IF NOT EXISTS "rounds_one_in_progress" ON "rounds" ("status") WHERE status = 'in_progress'`,
  )

  // For dev.db, try to find existing test user or create new one
  let testUser = await db.user.findUnique({
    where: { email: 'test@example.com' },
  })

  if (!testUser) {
    testUser = await db.user.create({
      data: {
        email: 'test@example.com',
        name: 'Test User',
      },
    })
  }

  async function resetDatabase() {
    await db.shot.deleteMany()
    await db.roundHole.deleteMany()
    await db.round.deleteMany()
    await db.courseHole.deleteMany()
    await db.course.deleteMany()
  }

  async function teardown() {
    await db.$disconnect()
    removeTestDatabaseFiles()
  }

  return {
    db,
    courses,
    rounds,
    testUser,
    resetDatabase,
    teardown,
  }
}
