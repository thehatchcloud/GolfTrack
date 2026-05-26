import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { addShot } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function POST(
  request: NextRequest,
  context: { params: Promise<{ id: string; holeNumber: string }> },
) {
  const token = await getToken({
    req: request,
    secret: process.env.AUTH_SECRET,
    secureCookie: request.url.startsWith('https://'),
  })

  const params = await context.params
  const roundId = parseId(params.id)
  const holeNumber = parseId(params.holeNumber)

  if (Number.isNaN(roundId) || Number.isNaN(holeNumber)) {
    return NextResponse.json({ error: 'Invalid round or hole id' }, { status: 400 })
  }

  try {
    const body = await request.json()
    const roundHole = await addShot(token!.sub as string, roundId, holeNumber, body.club)

    return NextResponse.json(roundHole)
  } catch (error) {
    return toResponse(error)
  }
}
