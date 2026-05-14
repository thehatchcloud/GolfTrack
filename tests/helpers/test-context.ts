import { execSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

const TEST_DB_FILENAME = 'test.db'
const TEST_DB_PATH = path.join(process.cwd(), 'prisma', TEST_DB_FILENAME)
const TEST_DB_JOURNAL_PATH = `${TEST_DB_PATH}-journal`
const TEST_DATABASE_URL = `file:./${TEST_DB_FILENAME}`

process.env.DATABASE_URL = TEST_DATABASE_URL

function removeTestDatabaseFiles() {
  for (const filePath of [TEST_DB_PATH, TEST_DB_JOURNAL_PATH]) {
    if (fs.existsSync(filePath)) {
      fs.rmSync(filePath, { force: true })
    }
  }
}

export async function setupTestContext() {
  removeTestDatabaseFiles()

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
    resetDatabase,
    teardown,
  }
}
