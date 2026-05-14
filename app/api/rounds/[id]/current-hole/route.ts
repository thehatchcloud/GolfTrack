import { NextResponse } from 'next/server'
import { ZodError } from 'zod'

import { setCurrentHole } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function PATCH(request: Request, context: { params: Promise<{ id: string }> }) {
  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid round id' }, { status: 400 })
  }

  try {
    const body = await request.json()
    const round = await setCurrentHole(id, body.currentHole)

    return NextResponse.json({ id: round.id, currentHole: round.currentHole })
  } catch (error) {
    if (error instanceof ZodError) {
      return NextResponse.json(
        {
          error: error.issues[0]?.message ?? 'Invalid current hole',
        },
        { status: 400 },
      )
    }

    if (error instanceof Error) {
      const status = error.message === 'Round not found' ? 404 : 400
      return NextResponse.json({ error: error.message }, { status })
    }

    return NextResponse.json({ error: 'Unable to update current hole' }, { status: 500 })
  }
}
