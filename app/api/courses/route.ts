import { NextResponse } from 'next/server'
import { ZodError } from 'zod'

import { createCourse, listCourses } from '@/lib/courses'

export async function GET() {
  const courses = await listCourses()
  return NextResponse.json(courses)
}

export async function POST(request: Request) {
  try {
    const body = await request.json()
    const course = await createCourse(body)

    return NextResponse.json({ id: course.id }, { status: 201 })
  } catch (error) {
    if (error instanceof ZodError) {
      return NextResponse.json(
        {
          error: error.issues[0]?.message ?? 'Invalid course data',
        },
        { status: 400 },
      )
    }

    return NextResponse.json({ error: 'Unable to create course' }, { status: 500 })
  }
}
