import test from 'node:test'
import assert from 'node:assert/strict'
import path from 'node:path'

// Set database and auth before importing services
const devDbPath = path.join(process.cwd(), 'prisma', 'dev.db')
process.env.DATABASE_URL = `file:${devDbPath}`
process.env.AUTH_SECRET = 'test-secret-key-for-testing-only'
// Use dev.db for E2E tests so the dev server can see the test data
process.env.TEST_USE_DEV_DB = 'true'

import request from 'supertest'
import { setupTestContext } from '../helpers/test-context'
import { sampleCourseInput } from '../helpers/sample-data'
import { createSessionCookie } from '../helpers/auth'

const BASE_URL = 'http://localhost:3000'
let ctx: Awaited<ReturnType<typeof setupTestContext>>

// Check if dev server is running
async function checkDevServer(): Promise<boolean> {
  try {
    console.log(`[test] Attempting to fetch ${BASE_URL}/health`)
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 2000)

    const response = await fetch(`${BASE_URL}/health`, {
      signal: controller.signal,
    })

    clearTimeout(timeoutId)
    console.log(`[test] Health check response status: ${response.status}`)
    return response.ok
  } catch (error) {
    console.error(
      `[test] Health check failed: ${error instanceof Error ? error.message : String(error)}`,
    )
    return false
  }
}

test.before(async () => {
  console.log('\n=== Starting E2E Test Suite ===')
  console.log('[test] Checking if dev server is running on port 3000...')

  const serverReady = await checkDevServer()
  if (!serverReady) {
    console.error(
      '[test] ERROR: Dev server is not running on http://localhost:3000',
    )
    console.error('[test] Please start the dev server with: npm run dev')
    process.exit(1)
  }

  console.log('[test] Dev server is ready')

  try {
    ctx = await setupTestContext()
    console.log('[test] Test context initialized')
  } catch (error) {
    console.error('[test] Failed to initialize test context:', error)
    process.exit(1)
  }
})

test.after(async () => {
  console.log('\n=== Cleaning up E2E tests ===')
  try {
    if (ctx) {
      await ctx.teardown()
      console.log('[test] Test context cleaned up')
    }
  } catch (error) {
    console.error('[test] Error during teardown:', error)
  }

  console.log('[test] Done')
  process.exit(0)
})

// POST /api/rounds

test('POST /api/rounds returns 201 with round id on success', async () => {
  await ctx.resetDatabase()
  const course = await ctx.courses.createCourse(sampleCourseInput)
  const cookie = await createSessionCookie(ctx.testUser.id)

  const res = await request(BASE_URL)
    .post('/api/rounds')
    .set('Cookie', cookie)
    .send({ courseId: course.id })

  assert.equal(res.status, 201)
  assert.ok(typeof res.body.id === 'number')
})

test('POST /api/rounds returns 404 when course does not exist', async () => {
  await ctx.resetDatabase()
  const cookie = await createSessionCookie(ctx.testUser.id)

  const res = await request(BASE_URL)
    .post('/api/rounds')
    .set('Cookie', cookie)
    .send({ courseId: 99999 })

  assert.equal(res.status, 404)
  assert.match(res.body.error, /Course not found/)
})

test('POST /api/rounds returns 409 when a round is already in progress', async () => {
  await ctx.resetDatabase()
  const course = await ctx.courses.createCourse(sampleCourseInput)
  await ctx.rounds.createRound(ctx.testUser.id, course.id)
  const cookie = await createSessionCookie(ctx.testUser.id)

  const res = await request(BASE_URL)
    .post('/api/rounds')
    .set('Cookie', cookie)
    .send({ courseId: course.id })

  assert.equal(res.status, 409)
  assert.match(res.body.error, /already in progress/)
})

// POST /api/rounds/:id/cancel

test('POST /api/rounds/:id/cancel returns 200 and cancels the round', async () => {
  await ctx.resetDatabase()
  const course = await ctx.courses.createCourse(sampleCourseInput)
  const round = await ctx.rounds.createRound(ctx.testUser.id, course.id)
  const cookie = await createSessionCookie(ctx.testUser.id)

  const res = await request(BASE_URL)
    .post(`/api/rounds/${round.id}/cancel`)
    .set('Cookie', cookie)

  assert.equal(res.status, 200)
  assert.deepEqual(res.body, { id: round.id, cancelled: true })
  assert.equal(await ctx.db.round.count(), 0)
})

test('POST /api/rounds/:id/cancel returns 404 when round does not exist', async () => {
  await ctx.resetDatabase()
  const cookie = await createSessionCookie(ctx.testUser.id)

  const res = await request(BASE_URL)
    .post('/api/rounds/99999/cancel')
    .set('Cookie', cookie)

  assert.equal(res.status, 404)
  assert.match(res.body.error, /Round not found/)
})

test('POST /api/rounds/:id/cancel returns 409 when round is already completed', async () => {
  await ctx.resetDatabase()
  const course = await ctx.courses.createCourse(sampleCourseInput)
  const round = await ctx.rounds.createRound(ctx.testUser.id, course.id)
  await ctx.rounds.completeRound(ctx.testUser.id, round.id)
  const cookie = await createSessionCookie(ctx.testUser.id)

  const res = await request(BASE_URL)
    .post(`/api/rounds/${round.id}/cancel`)
    .set('Cookie', cookie)

  assert.equal(res.status, 409)
  assert.match(res.body.error, /Only in-progress rounds can be cancelled/)
})
