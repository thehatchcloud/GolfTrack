import { db } from '@/lib/db'
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
    await tx.course.update({
      where: { id },
      data: {
        name: data.name,
        holeCount: data.holeCount,
      },
    })

    await tx.courseHole.deleteMany({
      where: { courseId: id },
    })

    await tx.courseHole.createMany({
      data: data.holes.map((hole) => ({
        courseId: id,
        holeNumber: hole.holeNumber,
        par: hole.par,
      })),
    })

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
