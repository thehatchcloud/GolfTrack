import test from 'node:test'
import assert from 'node:assert/strict'

import { setupTestContext } from './helpers/test-context'
import { sampleCourseInput, sampleEighteenHoleCourseInput } from './helpers/sample-data'

test('createRound snapshots course holes and starts at hole 1', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)

    const round = await ctx.rounds.createRound(course.id)

    assert.equal(round.status, 'in_progress')
    assert.equal(round.currentHole, 1)
    assert.equal(round.holes.length, 9)
    assert.deepEqual(
      round.holes.map((hole) => ({ holeNumber: hole.holeNumber, par: hole.par, strokes: hole.strokes })),
      sampleCourseInput.holes.map((hole) => ({ ...hole, strokes: 0 })),
    )
  } finally {
    await ctx.teardown()
  }
})

test('createRound can create a front 9 round from an 18-hole course', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleEighteenHoleCourseInput)

    const round = await ctx.rounds.createRound(course.id, 'front9')

    assert.equal(round.currentHole, 1)
    assert.deepEqual(
      round.holes.map((hole) => hole.holeNumber),
      [1, 2, 3, 4, 5, 6, 7, 8, 9],
    )
  } finally {
    await ctx.teardown()
  }
})

test('createRound can create a back 9 round from an 18-hole course', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleEighteenHoleCourseInput)

    const round = await ctx.rounds.createRound(course.id, 'back9')

    assert.equal(round.currentHole, 10)
    assert.deepEqual(
      round.holes.map((hole) => hole.holeNumber),
      [10, 11, 12, 13, 14, 15, 16, 17, 18],
    )
  } finally {
    await ctx.teardown()
  }
})

test('createRound rejects front 9 or back 9 on a 9-hole course', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)

    await assert.rejects(
      () => ctx.rounds.createRound(course.id, 'front9'),
      /Front 9 or back 9 is only available on 18-hole courses/,
    )
  } finally {
    await ctx.teardown()
  }
})

test('concurrent addShot calls both succeed without unique constraint violations', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    const [result1, result2] = await Promise.all([
      ctx.rounds.addShot(round.id, 1, 'Driver'),
      ctx.rounds.addShot(round.id, 1, '7i'),
    ])

    assert.ok(result1)
    assert.ok(result2)

    const finalRound = await ctx.rounds.getRoundById(round.id)
    const hole1 = finalRound?.holes.find((h) => h.holeNumber === 1)
    assert.equal(hole1?.strokes, 2)
    assert.equal(hole1?.shots.length, 2)
    assert.deepEqual(hole1?.shots.map((s) => s.shotNumber), [1, 2])
  } finally {
    await ctx.teardown()
  }
})

test('addShot increments strokes and appends ordered shots', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    await ctx.rounds.addShot(round.id, 1, 'Driver')
    const updatedHole = await ctx.rounds.addShot(round.id, 1, '7i')

    assert.ok(updatedHole)
    assert.equal(updatedHole?.strokes, 2)
    assert.deepEqual(
      updatedHole?.shots.map((shot) => ({ shotNumber: shot.shotNumber, club: shot.club })),
      [
        { shotNumber: 1, club: 'Driver' },
        { shotNumber: 2, club: '7i' },
      ],
    )
  } finally {
    await ctx.teardown()
  }
})

test('undoLastShot removes the latest shot and updates strokes', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    await ctx.rounds.addShot(round.id, 1, 'Driver')
    await ctx.rounds.addShot(round.id, 1, '7i')
    const holeAfterUndo = await ctx.rounds.undoLastShot(round.id, 1)

    assert.ok(holeAfterUndo)
    assert.equal(holeAfterUndo?.strokes, 1)
    assert.deepEqual(
      holeAfterUndo?.shots.map((shot) => ({ shotNumber: shot.shotNumber, club: shot.club })),
      [{ shotNumber: 1, club: 'Driver' }],
    )
  } finally {
    await ctx.teardown()
  }
})

test('deleteShot removes a middle shot, renumbers the rest, and updates strokes', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    await ctx.rounds.addShot(round.id, 1, 'Driver')
    await ctx.rounds.addShot(round.id, 1, '7i')
    await ctx.rounds.addShot(round.id, 1, 'PW')

    const roundBeforeDelete = await ctx.rounds.getRoundById(round.id)
    const shotToDelete = roundBeforeDelete?.holes.find((hole) => hole.holeNumber === 1)?.shots[1]

    assert.ok(shotToDelete)

    const holeAfterDelete = await ctx.rounds.deleteShot(round.id, 1, shotToDelete!.id)

    assert.ok(holeAfterDelete)
    assert.equal(holeAfterDelete?.strokes, 2)
    assert.deepEqual(
      holeAfterDelete?.shots.map((shot) => ({ shotNumber: shot.shotNumber, club: shot.club })),
      [
        { shotNumber: 1, club: 'Driver' },
        { shotNumber: 2, club: 'PW' },
      ],
    )
  } finally {
    await ctx.teardown()
  }
})

test('updateShot changes the club without changing shot order', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    await ctx.rounds.addShot(round.id, 1, 'Driver')
    const holeWithShot = await ctx.rounds.addShot(round.id, 1, '7i')
    const shotToUpdate = holeWithShot?.shots[1]

    assert.ok(shotToUpdate)

    await ctx.rounds.updateShot(round.id, 1, shotToUpdate!.id, '8i')
    const updatedRound = await ctx.rounds.getRoundById(round.id)
    const updatedHole = updatedRound?.holes.find((hole) => hole.holeNumber === 1)

    assert.deepEqual(
      updatedHole?.shots.map((shot) => ({ shotNumber: shot.shotNumber, club: shot.club })),
      [
        { shotNumber: 1, club: 'Driver' },
        { shotNumber: 2, club: '8i' },
      ],
    )
    assert.equal(updatedHole?.strokes, 2)
  } finally {
    await ctx.teardown()
  }
})

test('completeRound marks round completed and blocks future scoring changes', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    await ctx.rounds.addShot(round.id, 1, 'Driver')
    const completedRound = await ctx.rounds.completeRound(round.id, 'Finished strong')

    assert.equal(completedRound.status, 'completed')
    assert.equal(completedRound.note, 'Finished strong')
    assert.ok(completedRound.finishedAt)

    await assert.rejects(() => ctx.rounds.addShot(round.id, 1, '7i'), /Round is already completed/)
    await assert.rejects(() => ctx.rounds.undoLastShot(round.id, 1), /Round is already completed/)
    await assert.rejects(() => ctx.rounds.updateShot(round.id, 1, 1, 'PW'), /Round is already completed|Shot not found/)
  } finally {
    await ctx.teardown()
  }
})

test('cancelRound deletes an in-progress round and its related data', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    await ctx.rounds.addShot(round.id, 1, 'Driver')
    const result = await ctx.rounds.cancelRound(round.id)

    assert.deepEqual(result, { id: round.id, cancelled: true })
    assert.equal(await ctx.rounds.getRoundById(round.id), null)
    assert.equal(await ctx.rounds.getInProgressRound(), null)
    assert.equal(await ctx.db.round.count(), 0)
    assert.equal(await ctx.db.roundHole.count(), 0)
    assert.equal(await ctx.db.shot.count(), 0)
  } finally {
    await ctx.teardown()
  }
})

test('concurrent createRound only allows one in-progress round', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)

    const results = await Promise.allSettled([
      ctx.rounds.createRound(course.id),
      ctx.rounds.createRound(course.id),
    ])

    const fulfilled = results.filter((r) => r.status === 'fulfilled')
    const rejected = results.filter((r) => r.status === 'rejected')

    assert.equal(fulfilled.length, 1)
    assert.equal(rejected.length, 1)
    assert.match((rejected[0] as PromiseRejectedResult).reason.message, /A round is already in progress/)
    assert.equal(await ctx.db.round.count(), 1)
  } finally {
    await ctx.teardown()
  }
})

test('cancelRound rejects completed rounds', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    await ctx.rounds.completeRound(round.id, 'Done')

    await assert.rejects(() => ctx.rounds.cancelRound(round.id), /Only in-progress rounds can be cancelled/)
  } finally {
    await ctx.teardown()
  }
})
