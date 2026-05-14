import test from 'node:test'
import assert from 'node:assert/strict'

import { setupTestContext } from './helpers/test-context'
import { sampleCourseInput, updatedCourseInput } from './helpers/sample-data'

test('createCourse persists a course with ordered holes', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)

    assert.equal(course.name, sampleCourseInput.name)
    assert.equal(course.holeCount, 9)
    assert.equal(course.holes.length, 9)
    assert.deepEqual(
      course.holes.map((hole) => ({ holeNumber: hole.holeNumber, par: hole.par })),
      sampleCourseInput.holes,
    )
  } finally {
    await ctx.teardown()
  }
})

test('listCourses returns courses sorted by name with total par', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()

    await ctx.courses.createCourse(sampleCourseInput)
    await ctx.courses.createCourse({
      ...sampleCourseInput,
      name: 'Alpha Course',
      holes: sampleCourseInput.holes.map((hole) =>
        hole.holeNumber === 1 ? { ...hole, par: 5 } : hole,
      ),
    })

    const courses = await ctx.courses.listCourses()

    assert.deepEqual(
      courses.map((course) => course.name),
      ['Alpha Course', 'Test Valley'],
    )

    const alpha = courses[0]
    assert.equal(alpha.totalPar, sampleCourseInput.holes.reduce((sum, hole) => sum + hole.par, 0) + 1)
  } finally {
    await ctx.teardown()
  }
})

test('getCourseById returns null for a missing course', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.getCourseById(9999)
    assert.equal(course, null)
  } finally {
    await ctx.teardown()
  }
})

test('updateCourse replaces course metadata and hole pars', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const createdCourse = await ctx.courses.createCourse(sampleCourseInput)

    const updatedCourse = await ctx.courses.updateCourse(createdCourse.id, updatedCourseInput)

    assert.ok(updatedCourse)
    assert.equal(updatedCourse?.name, updatedCourseInput.name)
    assert.equal(updatedCourse?.holeCount, 9)
    assert.deepEqual(
      updatedCourse?.holes.map((hole) => ({ holeNumber: hole.holeNumber, par: hole.par })),
      updatedCourseInput.holes,
    )
  } finally {
    await ctx.teardown()
  }
})

test('course updates do not alter existing round hole snapshots', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const createdCourse = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(createdCourse.id)

    await ctx.courses.updateCourse(createdCourse.id, updatedCourseInput)

    const roundAfterUpdate = await ctx.rounds.getRoundById(round.id)

    assert.ok(roundAfterUpdate)
    assert.deepEqual(
      roundAfterUpdate?.holes.map((hole) => ({ holeNumber: hole.holeNumber, par: hole.par })),
      sampleCourseInput.holes,
    )
  } finally {
    await ctx.teardown()
  }
})

test('createCourse rejects invalid course input through service validation', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()

    await assert.rejects(
      () =>
        ctx.courses.createCourse({
          ...sampleCourseInput,
          holes: sampleCourseInput.holes.slice(0, 8),
        }),
      /Expected 9 holes/,
    )
  } finally {
    await ctx.teardown()
  }
})
