import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { createRound, listCompletedRounds } from '@/lib/rounds'

export async function GET(request: NextRequest) {
  const token = await getToken({
    req: request,
    secret: process.env.AUTH_SECRET,
    secureCookie: request.url.startsWith('https://'),
  })

  if (!token?.sub) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  const rounds = await listCompletedRounds(token.sub)
  return NextResponse.json(rounds)
}

export async function POST(request: NextRequest) {
  try {
    const token = await getToken({
      req: request,
      secret: process.env.AUTH_SECRET,
      secureCookie: request.url.startsWith('https://'),
    })

    if (!token?.sub) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    const body = await request.json()
    const round = await createRound(token.sub, body.courseId, body.playMode)

    return NextResponse.json({ id: round.id }, { status: 201 })
  } catch (error) {
    return toResponse(error)
  }
}
