import { NextResponse } from 'next/server'
import { ZodError } from 'zod'

import { getCourseById, updateCourse } from '@/lib/courses'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function GET(_: Request, context: { params: Promise<{ id: string }> }) {
  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid course id' }, { status: 400 })
  }

  const course = await getCourseById(id)

  if (!course) {
    return NextResponse.json({ error: 'Course not found' }, { status: 404 })
  }

  return NextResponse.json(course)
}

export async function PUT(request: Request, context: { params: Promise<{ id: string }> }) {
  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid course id' }, { status: 400 })
  }

  try {
    const body = await request.json()
    const course = await updateCourse(id, body)

    if (!course) {
      return NextResponse.json({ error: 'Course not found' }, { status: 404 })
    }

    return NextResponse.json({ id: course.id })
  } catch (error) {
    if (error instanceof ZodError) {
      return NextResponse.json(
        {
          error: error.issues[0]?.message ?? 'Invalid course data',
        },
        { status: 400 },
      )
    }

    return NextResponse.json({ error: 'Unable to update course' }, { status: 500 })
  }
}
