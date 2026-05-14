import { PrismaClient, RoundStatus } from '@prisma/client'

const prisma = new PrismaClient()

async function main() {
  if (process.env.NODE_ENV === 'production') {
    throw new Error('Refusing to seed in production')
  }
  const dbUrl = process.env.DATABASE_URL ?? ''
  if (!dbUrl.includes('dev.db') && !dbUrl.includes('test.db')) {
    throw new Error(`Refusing to seed: DATABASE_URL does not look like a dev DB (${dbUrl})`)
  }

  await prisma.shot.deleteMany()
  await prisma.roundHole.deleteMany()
  await prisma.round.deleteMany()
  await prisma.courseHole.deleteMany()
  await prisma.course.deleteMany()

  const sampleMunicipal = await prisma.course.create({
    data: {
      name: 'Sample Municipal',
      holeCount: 9,
      holes: {
        create: [
          { holeNumber: 1, par: 4 },
          { holeNumber: 2, par: 3 },
          { holeNumber: 3, par: 5 },
          { holeNumber: 4, par: 4 },
          { holeNumber: 5, par: 4 },
          { holeNumber: 6, par: 3 },
          { holeNumber: 7, par: 5 },
          { holeNumber: 8, par: 4 },
          { holeNumber: 9, par: 4 },
        ],
      },
    },
    include: { holes: true },
  })

  const lakeside = await prisma.course.create({
    data: {
      name: 'Lakeside Country Club',
      holeCount: 18,
      holes: {
        create: [
          { holeNumber: 1, par: 4 },
          { holeNumber: 2, par: 5 },
          { holeNumber: 3, par: 3 },
          { holeNumber: 4, par: 4 },
          { holeNumber: 5, par: 4 },
          { holeNumber: 6, par: 5 },
          { holeNumber: 7, par: 3 },
          { holeNumber: 8, par: 4 },
          { holeNumber: 9, par: 4 },
          { holeNumber: 10, par: 4 },
          { holeNumber: 11, par: 3 },
          { holeNumber: 12, par: 5 },
          { holeNumber: 13, par: 4 },
          { holeNumber: 14, par: 4 },
          { holeNumber: 15, par: 3 },
          { holeNumber: 16, par: 5 },
          { holeNumber: 17, par: 4 },
          { holeNumber: 18, par: 4 },
        ],
      },
    },
    include: { holes: true },
  })

  const completedRound = await prisma.round.create({
    data: {
      courseId: sampleMunicipal.id,
      status: RoundStatus.completed,
      currentHole: 9,
      startedAt: new Date('2026-05-10T14:00:00.000Z'),
      finishedAt: new Date('2026-05-10T16:10:00.000Z'),
      note: 'Driver was solid. Putting needs work.',
      holes: {
        create: [
          { holeNumber: 1, par: 4, strokes: 5 },
          { holeNumber: 2, par: 3, strokes: 3 },
          { holeNumber: 3, par: 5, strokes: 6 },
          { holeNumber: 4, par: 4, strokes: 4 },
          { holeNumber: 5, par: 4, strokes: 5 },
          { holeNumber: 6, par: 3, strokes: 3 },
          { holeNumber: 7, par: 5, strokes: 6 },
          { holeNumber: 8, par: 4, strokes: 5 },
          { holeNumber: 9, par: 4, strokes: 4 },
        ],
      },
    },
    include: { holes: true },
  })

  const hole1 = completedRound.holes.find((h) => h.holeNumber === 1)!
  const hole2 = completedRound.holes.find((h) => h.holeNumber === 2)!
  const hole3 = completedRound.holes.find((h) => h.holeNumber === 3)!

  await prisma.shot.createMany({
    data: [
      { roundHoleId: hole1.id, shotNumber: 1, club: 'Driver' },
      { roundHoleId: hole1.id, shotNumber: 2, club: '7i' },
      { roundHoleId: hole1.id, shotNumber: 3, club: 'PW' },
      { roundHoleId: hole1.id, shotNumber: 4, club: 'Putter' },
      { roundHoleId: hole1.id, shotNumber: 5, club: 'Putter' },
      { roundHoleId: hole2.id, shotNumber: 1, club: '8i' },
      { roundHoleId: hole2.id, shotNumber: 2, club: 'SW' },
      { roundHoleId: hole2.id, shotNumber: 3, club: 'Putter' },
      { roundHoleId: hole3.id, shotNumber: 1, club: 'Driver' },
      { roundHoleId: hole3.id, shotNumber: 2, club: '5i' },
      { roundHoleId: hole3.id, shotNumber: 3, club: '9i' },
      { roundHoleId: hole3.id, shotNumber: 4, club: 'SW' },
      { roundHoleId: hole3.id, shotNumber: 5, club: 'Putter' },
      { roundHoleId: hole3.id, shotNumber: 6, club: 'Putter' },
    ],
  })

  const inProgressRound = await prisma.round.create({
    data: {
      courseId: lakeside.id,
      status: RoundStatus.in_progress,
      currentHole: 4,
      startedAt: new Date('2026-05-12T13:30:00.000Z'),
      holes: {
        create: lakeside.holes.map((hole) => ({
          holeNumber: hole.holeNumber,
          par: hole.par,
          strokes:
            hole.holeNumber === 1
              ? 4
              : hole.holeNumber === 2
                ? 5
                : hole.holeNumber === 3
                  ? 3
                  : hole.holeNumber === 4
                    ? 2
                    : 0,
        })),
      },
    },
    include: { holes: true },
  })

  const ipHole1 = inProgressRound.holes.find((h) => h.holeNumber === 1)!
  const ipHole2 = inProgressRound.holes.find((h) => h.holeNumber === 2)!
  const ipHole3 = inProgressRound.holes.find((h) => h.holeNumber === 3)!
  const ipHole4 = inProgressRound.holes.find((h) => h.holeNumber === 4)!

  await prisma.shot.createMany({
    data: [
      { roundHoleId: ipHole1.id, shotNumber: 1, club: 'Driver' },
      { roundHoleId: ipHole1.id, shotNumber: 2, club: '8i' },
      { roundHoleId: ipHole1.id, shotNumber: 3, club: 'GW' },
      { roundHoleId: ipHole1.id, shotNumber: 4, club: 'Putter' },
      { roundHoleId: ipHole2.id, shotNumber: 1, club: 'Driver' },
      { roundHoleId: ipHole2.id, shotNumber: 2, club: '5i' },
      { roundHoleId: ipHole2.id, shotNumber: 3, club: '7i' },
      { roundHoleId: ipHole2.id, shotNumber: 4, club: 'SW' },
      { roundHoleId: ipHole2.id, shotNumber: 5, club: 'Putter' },
      { roundHoleId: ipHole3.id, shotNumber: 1, club: '6i' },
      { roundHoleId: ipHole3.id, shotNumber: 2, club: 'Putter' },
      { roundHoleId: ipHole3.id, shotNumber: 3, club: 'Putter' },
      { roundHoleId: ipHole4.id, shotNumber: 1, club: 'Driver' },
      { roundHoleId: ipHole4.id, shotNumber: 2, club: '9i' },
    ],
  })

  console.log('Seed complete')
  console.log({
    courses: [sampleMunicipal.name, lakeside.name],
    completedRoundId: completedRound.id,
    inProgressRoundId: inProgressRound.id,
  })
}

main()
  .catch((error) => {
    console.error('Seed failed:', error)
    process.exit(1)
  })
  .finally(async () => {
    await prisma.$disconnect()
  })
