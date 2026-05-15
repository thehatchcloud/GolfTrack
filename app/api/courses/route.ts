import { NextResponse } from 'next/server'

import { createCourse, listCourses } from '@/lib/courses'
import { toResponse } from '@/lib/errors'

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
    return toResponse(error)
  }
}
