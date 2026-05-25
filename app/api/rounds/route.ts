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

  const rounds = await listCompletedRounds(token!.sub as string)
  return NextResponse.json(rounds)
}

export async function POST(request: NextRequest) {
  try {
    const token = await getToken({
      req: request,
      secret: process.env.AUTH_SECRET,
      secureCookie: request.url.startsWith('https://'),
    })

    const body = await request.json()
    const round = await createRound(token!.sub as string, body.courseId, body.playMode)

    return NextResponse.json({ id: round.id }, { status: 201 })
  } catch (error) {
    return toResponse(error)
  }
}
