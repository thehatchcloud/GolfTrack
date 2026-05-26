import { encode } from 'next-auth/jwt'

const AUTH_SECRET = process.env.AUTH_SECRET || 'test-secret-key-for-testing-only'
const SESSION_COOKIE_NAME = 'authjs.session-token'

/**
 * Produces a real Auth.js v5 session cookie (encrypted JWE) for tests.
 * The route handlers' getToken() call decrypts this exactly like a browser cookie.
 */
export async function createSessionCookie(
  userId: string,
  role: 'USER' | 'ADMIN' = 'USER',
): Promise<string> {
  const token = await encode({
    token: { sub: userId, role },
    secret: AUTH_SECRET,
    salt: SESSION_COOKIE_NAME,
  })
  return `${SESSION_COOKIE_NAME}=${token}`
}
