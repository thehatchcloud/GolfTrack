import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { createRound, listCompletedRounds } from '@/lib/rounds'

function parseAuthorizationToken(request: NextRequest): { sub?: string } | null {
  const authHeader = request.headers.get('authorization')
  if (!authHeader) {
    return null
  }

  if (!authHeader.startsWith('Bearer ')) {
    return null
  }

  const token = authHeader.slice(7)
  try {
    const parts = token.split('.')
    if (parts.length !== 3) {
      return null
    }

    const payload = JSON.parse(Buffer.from(parts[1], 'base64url').toString())
    return payload
  } catch (error) {
    return null
  }
}

export async function GET(request: NextRequest) {
  let token = parseAuthorizationToken(request)

  if (!token) {
    token = await getToken({
      req: request,
      secret: process.env.AUTH_SECRET,
      secureCookie: request.url.startsWith('https://'),
    })
  }

  const rounds = await listCompletedRounds(token!.sub as string)
  return NextResponse.json(rounds)
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

    const body = await request.json()
    const round = await createRound(token.sub as string, body.courseId, body.playMode)

    return NextResponse.json({ id: round.id }, { status: 201 })
  } catch (error) {
    return toResponse(error)
  }
}
