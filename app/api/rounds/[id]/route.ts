import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'

import { getRoundById } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function GET(request: NextRequest, context: { params: Promise<{ id: string }> }) {
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

  const round = await getRoundById(token!.sub as string, id)

  if (!round) {
    return NextResponse.json({ error: 'Round not found' }, { status: 404 })
  }

  return NextResponse.json(round)
}
