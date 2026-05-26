import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { cancelRound } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

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

export async function POST(request: NextRequest, context: { params: Promise<{ id: string }> }) {
  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid round id' }, { status: 400 })
  }

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

  try {
    const result = await cancelRound(token.sub as string, id)
    return NextResponse.json(result)
  } catch (error) {
    return toResponse(error)
  }
}
