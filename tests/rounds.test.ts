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

test('concurrent updateShot and deleteShot on the same shot leave data consistent', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    await ctx.rounds.addShot(round.id, 1, 'Driver')
    await ctx.rounds.addShot(round.id, 1, '7i')

    const roundBeforeRace = await ctx.rounds.getRoundById(round.id)
    const shotToRace = roundBeforeRace!.holes.find((h) => h.holeNumber === 1)!.shots[0]

    const results = await Promise.allSettled([
      ctx.rounds.updateShot(round.id, 1, shotToRace.id, '5i'),
      ctx.rounds.deleteShot(round.id, 1, shotToRace.id),
    ])

    const fulfilled = results.filter((r) => r.status === 'fulfilled')
    assert.ok(fulfilled.length >= 1, 'at least one operation must succeed')

    for (const r of results.filter((r) => r.status === 'rejected')) {
      assert.match((r as PromiseRejectedResult).reason.message, /Shot not found/)
    }

    const finalHole = (await ctx.rounds.getRoundById(round.id))!.holes.find((h) => h.holeNumber === 1)!
    assert.equal(finalHole.strokes, 1, 'only the 7i shot remains after deletion')
    assert.equal(finalHole.shots.length, 1)
    assert.equal(finalHole.shots[0].shotNumber, 1)
    assert.equal(finalHole.shots[0].club, '7i')
  } finally {
    await ctx.teardown()
  }
})

test('concurrent deleteShot on the same shot allows only one to succeed', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    await ctx.rounds.addShot(round.id, 1, 'Driver')
    await ctx.rounds.addShot(round.id, 1, '7i')

    const roundBeforeDelete = await ctx.rounds.getRoundById(round.id)
    const shotId = roundBeforeDelete!.holes.find((h) => h.holeNumber === 1)!.shots[0].id

    const results = await Promise.allSettled([
      ctx.rounds.deleteShot(round.id, 1, shotId),
      ctx.rounds.deleteShot(round.id, 1, shotId),
    ])

    const fulfilled = results.filter((r) => r.status === 'fulfilled')
    const rejected = results.filter((r) => r.status === 'rejected')

    assert.equal(fulfilled.length, 1)
    assert.equal(rejected.length, 1)
    assert.match((rejected[0] as PromiseRejectedResult).reason.message, /Shot not found/)

    const finalRound = await ctx.rounds.getRoundById(round.id)
    const hole1 = finalRound?.holes.find((h) => h.holeNumber === 1)
    assert.equal(hole1?.strokes, 1)
    assert.equal(hole1?.shots.length, 1)
    assert.deepEqual(hole1?.shots.map((s) => s.shotNumber), [1])
  } finally {
    await ctx.teardown()
  }
})

test('concurrent completeRound and addShot never leaves a completed round with shots added after finishedAt', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    const results = await Promise.allSettled([
      ctx.rounds.completeRound(round.id, 'Done'),
      ctx.rounds.addShot(round.id, 1, 'Driver'),
    ])

    const fulfilled = results.filter((r) => r.status === 'fulfilled')
    assert.ok(fulfilled.length >= 1, 'at least one operation must succeed')

    for (const r of results.filter((r) => r.status === 'rejected')) {
      assert.match((r as PromiseRejectedResult).reason.message, /Round is already completed/)
    }

    const finalRound = await ctx.rounds.getRoundById(round.id)
    assert.ok(finalRound, 'round still exists')
    assert.equal(finalRound!.status, 'completed')
    assert.ok(finalRound!.finishedAt, 'finishedAt is set')

    const allShots = finalRound!.holes.flatMap((h) => h.shots)
    for (const shot of allShots) {
      assert.ok(
        shot.createdAt <= finalRound!.finishedAt!,
        `shot createdAt (${shot.createdAt.toISOString()}) must not be after finishedAt (${finalRound!.finishedAt!.toISOString()})`,
      )
    }
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

test('createRound stores playMode on the round', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleEighteenHoleCourseInput)

    const full = await ctx.rounds.createRound(course.id, 'full')
    assert.equal(full.playMode, 'full')
    await ctx.rounds.cancelRound(full.id)

    const front9 = await ctx.rounds.createRound(course.id, 'front9')
    assert.equal(front9.playMode, 'front9')
    await ctx.rounds.cancelRound(front9.id)

    const back9 = await ctx.rounds.createRound(course.id, 'back9')
    assert.equal(back9.playMode, 'back9')
    await ctx.rounds.cancelRound(back9.id)
  } finally {
    await ctx.teardown()
  }
})

test('completeRound stores totalStrokes, totalPar, and relativeToPar', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    // sampleCourseInput: 9 holes, total par = 36
    const course = await ctx.courses.createCourse(sampleCourseInput)
    const round = await ctx.rounds.createRound(course.id)

    // Add 5 shots to hole 1 (par 4), leave other holes at 0 strokes
    for (let i = 0; i < 5; i++) {
      await ctx.rounds.addShot(round.id, 1, 'Driver')
    }

    const completed = await ctx.rounds.completeRound(round.id)

    assert.equal(completed.totalStrokes, 5)
    assert.equal(completed.totalPar, 36)
    assert.equal(completed.relativeToPar, 5 - 36)
  } finally {
    await ctx.teardown()
  }
})

test('listCompletedRounds returns stored totals without holes data', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)

    const round = await ctx.rounds.createRound(course.id)
    await ctx.rounds.addShot(round.id, 1, 'Driver')
    await ctx.rounds.addShot(round.id, 1, '7i')
    await ctx.rounds.completeRound(round.id)

    const rounds = await ctx.rounds.listCompletedRounds()

    assert.equal(rounds.length, 1)
    assert.equal(rounds[0].totalStrokes, 2)
    assert.equal(rounds[0].totalPar, 36)
    assert.equal(rounds[0].relativeToPar, 2 - 36)
    assert.ok(!('holes' in rounds[0]), 'holes should not be included in list')
  } finally {
    await ctx.teardown()
  }
})

test('listCompletedRounds paginates with limit and page', async () => {
  const ctx = await setupTestContext()

  try {
    await ctx.resetDatabase()
    const course = await ctx.courses.createCourse(sampleCourseInput)

    // Create and complete 3 rounds
    for (let i = 0; i < 3; i++) {
      const round = await ctx.rounds.createRound(course.id)
      await ctx.rounds.completeRound(round.id)
    }

    const page0 = await ctx.rounds.listCompletedRounds(2, 0)
    const page1 = await ctx.rounds.listCompletedRounds(2, 1)
    const page2 = await ctx.rounds.listCompletedRounds(2, 2)

    assert.equal(page0.length, 2)
    assert.equal(page1.length, 1)
    assert.equal(page2.length, 0)
    // pages should not overlap
    assert.ok(page0[0].id !== page1[0].id)
  } finally {
    await ctx.teardown()
  }
})
