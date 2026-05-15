import { db } from '@/lib/db'
import { NotFoundError } from '@/lib/errors'
import { courseInputSchema, type CourseInput } from '@/lib/validation'

export async function listCourses() {
  const courses = await db.course.findMany({
    include: {
      holes: {
        orderBy: { holeNumber: 'asc' },
      },
    },
    orderBy: { name: 'asc' },
  })

  return courses.map((course) => ({
    ...course,
    totalPar: course.holes.reduce((sum, hole) => sum + hole.par, 0),
  }))
}

export async function getCourseById(id: number) {
  return db.course.findUnique({
    where: { id },
    include: {
      holes: {
        orderBy: { holeNumber: 'asc' },
      },
    },
  })
}

export async function createCourse(input: CourseInput) {
  const data = courseInputSchema.parse(input)

  return db.$transaction(async (tx) => {
    return tx.course.create({
      data: {
        name: data.name,
        holeCount: data.holeCount,
        holes: {
          create: data.holes.map((hole) => ({
            holeNumber: hole.holeNumber,
            par: hole.par,
          })),
        },
      },
      include: {
        holes: {
          orderBy: { holeNumber: 'asc' },
        },
      },
    })
  })
}

export async function updateCourse(id: number, input: CourseInput) {
  const data = courseInputSchema.parse(input)

  return db.$transaction(async (tx) => {
    const existing = await tx.course.findUnique({
      where: { id },
      include: { holes: true },
    })
    if (!existing) {
      throw new NotFoundError('Course not found')
    }

    await tx.course.update({
      where: { id },
      data: {
        name: data.name,
        holeCount: data.holeCount,
      },
    })

    const existingByHole = new Map(existing.holes.map((h) => [h.holeNumber, h]))
    const incomingByHole = new Map(data.holes.map((h) => [h.holeNumber, h]))

    for (const hole of data.holes) {
      const ex = existingByHole.get(hole.holeNumber)
      if (ex) {
        if (ex.par !== hole.par) {
          await tx.courseHole.update({ where: { id: ex.id }, data: { par: hole.par } })
        }
      } else {
        await tx.courseHole.create({
          data: { courseId: id, holeNumber: hole.holeNumber, par: hole.par },
        })
      }
    }

    const removedIds = existing.holes
      .filter((h) => !incomingByHole.has(h.holeNumber))
      .map((h) => h.id)

    if (removedIds.length > 0) {
      await tx.courseHole.deleteMany({ where: { id: { in: removedIds } } })
    }

    return tx.course.findUnique({
      where: { id },
      include: {
        holes: {
          orderBy: { holeNumber: 'asc' },
        },
      },
    })
  })
}
