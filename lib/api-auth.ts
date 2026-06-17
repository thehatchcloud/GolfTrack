import { getToken } from 'next-auth/jwt'
import { NextRequest, NextResponse } from 'next/server'
import type { UserRole } from '@prisma/client'

export interface SessionToken {
  sub?: string
  role?: UserRole
}

/**
 * Decrypts the Auth.js session token from the request cookie. Mirrors the
 * `getToken()` call used across the API routes so cookie name / decryption
 * salt are derived identically in dev (http) and prod (https).
 */
export async function getRequestToken(request: NextRequest): Promise<SessionToken | null> {
  return getToken({
    req: request,
    secret: process.env.AUTH_SECRET,
    secureCookie: request.url.startsWith('https://'),
  })
}

type AuthResult = { token: SessionToken } | { response: NextResponse }

/**
 * Requires an authenticated admin. Returns the decoded token on success, or a
 * ready-to-return NextResponse: 401 when unauthenticated, 403 when the caller
 * is signed in but not an admin.
 *
 * Usage:
 *   const auth = await requireAdmin(request)
 *   if ('response' in auth) return auth.response
 */
export async function requireAdmin(request: NextRequest): Promise<AuthResult> {
  const token = await getRequestToken(request)

  if (!token?.sub) {
    return { response: NextResponse.json({ error: 'Unauthorized' }, { status: 401 }) }
  }

  if (token.role !== 'ADMIN') {
    return { response: NextResponse.json({ error: 'Forbidden' }, { status: 403 }) }
  }

  return { token }
}
