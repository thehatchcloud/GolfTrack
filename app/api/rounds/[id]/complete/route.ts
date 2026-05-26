import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { completeRound } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function POST(request: NextRequest, context: { params: Promise<{ id: string }> }) {
  const token = await getToken({
    req: request,
    secret: process.env.AUTH_SECRET,
    secureCookie: request.url.startsWith('https://'),
  })

  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid round id' }, { status: 400 })
  }

  try {
    const body = await request.json().catch(() => ({}))
    const round = await completeRound(token!.sub as string, id, body.note)

    return NextResponse.json({ id: round.id })
  } catch (error) {
    return toResponse(error)
  }
}
