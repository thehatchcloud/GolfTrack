import { encode } from 'next-auth/jwt'

const SESSION_COOKIE_NAME = 'authjs.session-token'

/**
 * Produces a real Auth.js v5 session cookie (encrypted JWE) for tests.
 * The route handlers' getToken() call decrypts this exactly like a browser cookie.
 *
 * AUTH_SECRET is read at call time, not at module init, so it picks up env
 * changes made after this module is imported (ESM hoists imports above
 * top-level statements, including loadEnvConfig() and process.env assignments).
 */
export async function createSessionCookie(
  userId: string,
  role: 'USER' | 'ADMIN' = 'USER',
): Promise<string> {
  const secret = process.env.AUTH_SECRET || 'test-secret-key-for-testing-only'
  const token = await encode({
    token: { sub: userId, role },
    secret,
    salt: SESSION_COOKIE_NAME,
  })
  return `${SESSION_COOKIE_NAME}=${token}`
}
