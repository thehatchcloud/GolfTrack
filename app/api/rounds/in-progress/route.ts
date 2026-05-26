import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'

import { getInProgressRound } from '@/lib/rounds'

export async function GET(request: NextRequest) {
  const token = await getToken({
    req: request,
    secret: process.env.AUTH_SECRET,
    secureCookie: request.url.startsWith('https://'),
  })

  const round = await getInProgressRound(token!.sub as string)
  return NextResponse.json(round)
}
