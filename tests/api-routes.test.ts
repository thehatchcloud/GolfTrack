import test from 'node:test'
import assert from 'node:assert/strict'

// Must import before route handlers so DATABASE_URL is set before Prisma client init
import { setupTestContext } from './helpers/test-context'
import { sampleCourseInput, updatedCourseInput } from './helpers/sample-data'

import { POST as roundsPost } from '../app/api/rounds/route'
import { POST as cancelPost } from '../app/api/rounds/[id]/cancel/route'
import { PUT as coursePut } from '../app/api/courses/[id]/route'

function jsonRequest(body: unknown): Request {
  return new Request('http://localhost', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

function routeContext(id: string | number) {
  return { params: Promise.resolve({ id: String(id) }) }
}

// POST /api/rounds

test('POST /api/rounds returns 201 with round id on success', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)

    const res = await roundsPost(jsonRequest({ courseId: course.id }))
    const body = await res.json()

    assert.equal(res.status, 201)
    assert.ok(typeof body.id === 'number')
  } finally {
    await ctx.teardown()
  }
})

test('POST /api/rounds returns 404 when course does not exist', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()

    const res = await roundsPost(jsonRequest({ courseId: 99999 }))
    const body = await res.json()

    assert.equal(res.status, 404)
    assert.match(body.error, /Course not found/)
  } finally {
    await ctx.teardown()
  }
})

test('POST /api/rounds returns 409 when a round is already in progress', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    await ctx.rounds.createRound(course.id)

    const res = await roundsPost(jsonRequest({ courseId: course.id }))
    const body = await res.json()

    assert.equal(res.status, 409)
    assert.match(body.error, /already in progress/)
  } finally {
    await ctx.teardown()
  }
})

// POST /api/rounds/:id/cancel

test('POST /api/rounds/:id/cancel returns 200 and cancels the round', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    const res = await cancelPost(new Request('http://localhost', { method: 'POST' }), routeContext(round.id))
    const body = await res.json()

    assert.equal(res.status, 200)
    assert.deepEqual(body, { id: round.id, cancelled: true })
    assert.equal(await ctx.db.round.count(), 0)
  } finally {
    await ctx.teardown()
  }
})

test('POST /api/rounds/:id/cancel returns 400 for a non-numeric id', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()

    const res = await cancelPost(new Request('http://localhost', { method: 'POST' }), routeContext('abc'))
    const body = await res.json()

    assert.equal(res.status, 400)
    assert.match(body.error, /Invalid round id/)
  } finally {
    await ctx.teardown()
  }
})

test('POST /api/rounds/:id/cancel returns 404 when round does not exist', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()

    const res = await cancelPost(new Request('http://localhost', { method: 'POST' }), routeContext(99999))
    const body = await res.json()

    assert.equal(res.status, 404)
    assert.match(body.error, /Round not found/)
  } finally {
    await ctx.teardown()
  }
})

test('POST /api/rounds/:id/cancel returns 409 when round is already completed', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)
    await ctx.rounds.completeRound(round.id)

    const res = await cancelPost(new Request('http://localhost', { method: 'POST' }), routeContext(round.id))
    const body = await res.json()

    assert.equal(res.status, 409)
    assert.match(body.error, /Only in-progress rounds can be cancelled/)
  } finally {
    await ctx.teardown()
  }
})

// PUT /api/courses/:id

test('PUT /api/courses/:id returns 200 and updates the course', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)

    const res = await coursePut(jsonRequest(updatedCourseInput), routeContext(course.id))
    const body = await res.json()

    assert.equal(res.status, 200)
    assert.equal(body.id, course.id)
  } finally {
    await ctx.teardown()
  }
})

test('PUT /api/courses/:id returns 404 when course does not exist', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()

    const res = await coursePut(jsonRequest(updatedCourseInput), routeContext(99999))
    const body = await res.json()

    assert.equal(res.status, 404)
    assert.match(body.error, /Course not found/)
  } finally {
    await ctx.teardown()
  }
})

test('PUT /api/courses/:id returns 400 for invalid course data', async () => {
  const ctx = await setupTestContext()
  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)

    const res = await coursePut(jsonRequest({ name: '', holeCount: 9, holes: [] }), routeContext(course.id))
    const body = await res.json()

    assert.equal(res.status, 400)
    assert.ok(body.error)
  } finally {
    await ctx.teardown()
  }
})
