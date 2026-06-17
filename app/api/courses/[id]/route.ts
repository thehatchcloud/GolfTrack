import { NextRequest, NextResponse } from 'next/server'

import { requireAdmin } from '@/lib/api-auth'
import { getCourseById, updateCourse } from '@/lib/courses'
import { toResponse } from '@/lib/errors'

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

export async function PUT(request: NextRequest, context: { params: Promise<{ id: string }> }) {
  const auth = await requireAdmin(request)
  if ('response' in auth) return auth.response

  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid course id' }, { status: 400 })
  }

  try {
    const body = await request.json()
    const course = await updateCourse(id, body)

    return NextResponse.json({ id: course?.id })
  } catch (error) {
    return toResponse(error)
  }
}
