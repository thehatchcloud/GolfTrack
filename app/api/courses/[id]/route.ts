import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'

import { getCourseById, updateCourse } from '@/lib/courses'
import { toResponse } from '@/lib/errors'

function parseAuthorizationToken(request: NextRequest): { sub?: string; role?: string } | null {
  const authHeader = request.headers.get('authorization')
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return null
  }

  const token = authHeader.slice(7)
  try {
    const parts = token.split('.')
    if (parts.length !== 3) {
      return null
    }

    return JSON.parse(Buffer.from(parts[1], 'base64url').toString())
  } catch {
    return null
  }
}

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function GET(_: NextRequest, context: { params: Promise<{ id: string }> }) {
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
  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid course id' }, { status: 400 })
  }

  try {
    let token = parseAuthorizationToken(request)

    if (!token) {
      token = await getToken({
        req: request,
        secret: process.env.AUTH_SECRET,
        secureCookie: request.url.startsWith('https://'),
      })
    }

    if (!token || !token.sub) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    if (token.role !== 'ADMIN') {
      return NextResponse.json({ error: 'Forbidden' }, { status: 403 })
    }

    const body = await request.json()
    const course = await updateCourse(id, body)

    return NextResponse.json({ id: course?.id })
  } catch (error) {
    return toResponse(error)
  }
}
