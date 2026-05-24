import { Prisma, RoundStatus } from '@prisma/client'

import { db } from '@/lib/db'
import { ConflictError, NotFoundError, ValidationError } from '@/lib/errors'
import { getCurrentHolePosition } from '@/lib/round-play'
import { calculateRoundTotals } from '@/lib/scoring'
import {
  roundCompletionInputSchema,
  setCurrentHoleInputSchema,
  shotInputSchema,
  startRoundInputSchema,
  updateShotInputSchema,
} from '@/lib/validation'


export async function getInProgressRound() {
  return db.round.findFirst({
    where: { status: RoundStatus.in_progress },
    include: {
      course: true,
      holes: {
        orderBy: { holeNumber: 'asc' },
      },
    },
    orderBy: { startedAt: 'desc' },
  })
}

export async function listCompletedRounds(limit?: number) {
  const rounds = await db.round.findMany({
    where: { status: RoundStatus.completed },
    include: {
      course: true,
      holes: {
        orderBy: { holeNumber: 'asc' },
      },
    },
    orderBy: [{ finishedAt: 'desc' }, { startedAt: 'desc' }],
    ...(limit !== undefined && { take: limit }),
  })

  return rounds.map((round) => {
    const totals = calculateRoundTotals(round.holes)

    return {
      ...round,
      ...totals,
    }
  })
}

export async function getRoundById(id: number) {
  return db.round.findUnique({
    where: { id },
    include: {
      course: true,
      holes: {
        include: {
          shots: {
            orderBy: { shotNumber: 'asc' },
          },
        },
        orderBy: { holeNumber: 'asc' },
      },
    },
  })
}

export async function createRound(courseId: number, playMode: 'full' | 'front9' | 'back9' = 'full') {
  const parsed = startRoundInputSchema.parse({ courseId, playMode })

  const existingRound = await getInProgressRound()
  if (existingRound) {
    throw new ConflictError('A round is already in progress')
  }

  const course = await db.course.findUnique({
    where: { id: parsed.courseId },
    include: {
      holes: {
        orderBy: { holeNumber: 'asc' },
      },
    },
  })

  if (!course) {
    throw new NotFoundError('Course not found')
  }

  if (course.holeCount !== 18 && parsed.playMode !== 'full') {
    throw new ValidationError('Front 9 or back 9 is only available on 18-hole courses')
  }

  const selectedHoles =
    parsed.playMode === 'front9'
      ? course.holes.filter((hole) => hole.holeNumber >= 1 && hole.holeNumber <= 9)
      : parsed.playMode === 'back9'
        ? course.holes.filter((hole) => hole.holeNumber >= 10 && hole.holeNumber <= 18)
        : course.holes

  const startingHole = selectedHoles[0]?.holeNumber ?? 1

  return db.$transaction(async (tx) => {
    const existing = await tx.round.findFirst({ where: { status: RoundStatus.in_progress } })
    if (existing) {
      throw new ConflictError('A round is already in progress')
    }

    return tx.round.create({
      data: {
        courseId: course.id,
        status: RoundStatus.in_progress,
        currentHole: startingHole,
        holes: {
          create: selectedHoles.map((hole) => ({
            holeNumber: hole.holeNumber,
            par: hole.par,
            strokes: 0,
          })),
        },
      },
      include: {
        course: true,
        holes: {
          orderBy: { holeNumber: 'asc' },
        },
      },
    })
  })
}

export async function setCurrentHole(roundId: number, currentHole: number) {
  const parsed = setCurrentHoleInputSchema.parse({ currentHole })

  const round = await db.round.findUnique({
    where: { id: roundId },
    include: {
      course: true,
      holes: {
        orderBy: { holeNumber: 'asc' },
      },
    },
  })

  if (!round) {
    throw new NotFoundError('Round not found')
  }

  if (round.status !== RoundStatus.in_progress) {
    throw new ConflictError('Round is already completed')
  }

  const availableHoleNumbers = round.holes.map((hole) => hole.holeNumber)
  if (!availableHoleNumbers.includes(parsed.currentHole)) {
    throw new ValidationError('Invalid hole number')
  }

  return db.round.update({
    where: { id: roundId },
    data: { currentHole: parsed.currentHole },
  })
}

export async function addShot(roundId: number, holeNumber: number, club: string) {
  const parsed = shotInputSchema.parse({ club })

  return db.$transaction(async (tx) => {
    const roundHole = await tx.roundHole.findFirst({
      where: { roundId, holeNumber },
      include: {
        round: true,
        shots: { orderBy: { shotNumber: 'asc' } },
      },
    })

    if (!roundHole) throw new NotFoundError('Round hole not found')
    if (roundHole.round.status !== RoundStatus.in_progress) throw new ConflictError('Round is already completed')

    const nextShotNumber = (roundHole.shots.at(-1)?.shotNumber ?? 0) + 1

    await tx.shot.create({
      data: {
        roundHoleId: roundHole.id,
        shotNumber: nextShotNumber,
        club: parsed.club,
      },
    })

    await tx.roundHole.update({
      where: { id: roundHole.id },
      data: { strokes: nextShotNumber },
    })

    return tx.roundHole.findUnique({
      where: { id: roundHole.id },
      include: {
        shots: { orderBy: { shotNumber: 'asc' } },
      },
    })
  }, { isolationLevel: Prisma.TransactionIsolationLevel.Serializable })
}

export async function undoLastShot(roundId: number, holeNumber: number) {
  return db.$transaction(async (tx) => {
    const roundHole = await tx.roundHole.findFirst({
      where: { roundId, holeNumber },
      include: {
        round: true,
        shots: { orderBy: { shotNumber: 'asc' } },
      },
    })

    if (!roundHole) throw new NotFoundError('Round hole not found')
    if (roundHole.round.status !== RoundStatus.in_progress) throw new ConflictError('Round is already completed')

    const latestShot = roundHole.shots.at(-1)

    if (!latestShot) {
      return tx.roundHole.findUnique({
        where: { id: roundHole.id },
        include: {
          shots: { orderBy: { shotNumber: 'asc' } },
        },
      })
    }

    await tx.shot.delete({
      where: { id: latestShot.id },
    })

    await tx.roundHole.update({
      where: { id: roundHole.id },
      data: { strokes: latestShot.shotNumber - 1 },
    })

    return tx.roundHole.findUnique({
      where: { id: roundHole.id },
      include: {
        shots: { orderBy: { shotNumber: 'asc' } },
      },
    })
  }, { isolationLevel: Prisma.TransactionIsolationLevel.Serializable })
}

export async function updateShot(roundId: number, holeNumber: number, shotId: number, club: string) {
  const parsed = updateShotInputSchema.parse({ club })

  return db.$transaction(async (tx) => {
    const roundHole = await tx.roundHole.findFirst({
      where: { roundId, holeNumber },
      include: {
        round: true,
        shots: { orderBy: { shotNumber: 'asc' } },
      },
    })

    if (!roundHole) throw new NotFoundError('Round hole not found')
    if (roundHole.round.status !== RoundStatus.in_progress) throw new ConflictError('Round is already completed')

    const shot = roundHole.shots.find((item) => item.id === shotId)
    if (!shot) throw new NotFoundError('Shot not found')

    return tx.shot.update({
      where: { id: shotId },
      data: { club: parsed.club },
    })
  }, { isolationLevel: Prisma.TransactionIsolationLevel.Serializable })
}

export async function deleteShot(roundId: number, holeNumber: number, shotId: number) {
  return db.$transaction(async (tx) => {
    const roundHole = await tx.roundHole.findFirst({
      where: { roundId, holeNumber },
      include: {
        round: true,
        shots: { orderBy: { shotNumber: 'asc' } },
      },
    })

    if (!roundHole) throw new NotFoundError('Round hole not found')
    if (roundHole.round.status !== RoundStatus.in_progress) throw new ConflictError('Round is already completed')

    const shot = roundHole.shots.find((item) => item.id === shotId)
    if (!shot) throw new NotFoundError('Shot not found')

    await tx.shot.delete({
      where: { id: shotId },
    })

    await tx.$executeRaw`
      UPDATE shots
      SET shot_number = shot_number - 1
      WHERE round_hole_id = ${roundHole.id} AND shot_number > ${shot.shotNumber}
    `

    await tx.roundHole.update({
      where: { id: roundHole.id },
      data: { strokes: roundHole.shots.length - 1 },
    })

    return tx.roundHole.findUnique({
      where: { id: roundHole.id },
      include: {
        shots: {
          orderBy: { shotNumber: 'asc' },
        },
      },
    })
  }, { isolationLevel: Prisma.TransactionIsolationLevel.Serializable })
}

export async function completeRound(roundId: number, note?: string | null) {
  const parsed = roundCompletionInputSchema.parse({ note })

  const round = await db.round.findUnique({
    where: { id: roundId },
  })

  if (!round) {
    throw new NotFoundError('Round not found')
  }

  if (round.status !== RoundStatus.in_progress) {
    throw new ConflictError('Round is already completed')
  }

  return db.round.update({
    where: { id: roundId },
    data: {
      status: RoundStatus.completed,
      finishedAt: new Date(),
      note: parsed.note ?? null,
    },
  })
}

export async function cancelRound(roundId: number) {
  return db.$transaction(async (tx) => {
    const result = await tx.round.deleteMany({
      where: { id: roundId, status: RoundStatus.in_progress },
    })

    if (result.count === 0) {
      const existing = await tx.round.findUnique({ where: { id: roundId } })
      if (!existing) throw new NotFoundError('Round not found')
      throw new ConflictError('Only in-progress rounds can be cancelled')
    }

    return { id: roundId, cancelled: true }
  })
}

export function getRoundPosition(round: {
  holes: Array<{ holeNumber: number }>
  currentHole: number
}) {
  return getCurrentHolePosition(
    round.holes.map((hole) => hole.holeNumber),
    round.currentHole,
  )
}
