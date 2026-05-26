import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'

import { createCourse, listCourses } from '@/lib/courses'
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

export async function GET() {
  const courses = await listCourses()
  return NextResponse.json(courses)
}

export async function POST(request: NextRequest) {
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
    const course = await createCourse(body)

    return NextResponse.json({ id: course.id }, { status: 201 })
  } catch (error) {
    return toResponse(error)
  }
}
