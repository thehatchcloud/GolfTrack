import crypto from 'node:crypto'

const AUTH_SECRET = process.env.AUTH_SECRET || 'test-secret-key-for-testing-only'

function base64urlEncode(data: string): string {
  return Buffer.from(data)
    .toString('base64')
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=/g, '')
}

function sign(message: string, secret: string): string {
  return base64urlEncode(
    crypto
      .createHmac('sha256', secret)
      .update(message)
      .digest('base64')
  )
}

export function createAuthToken(userId: string): string {
  const header = base64urlEncode(JSON.stringify({ alg: 'HS256', typ: 'JWT' }))
  const payload = base64urlEncode(
    JSON.stringify({
      sub: userId,
      iat: Math.floor(Date.now() / 1000),
      exp: Math.floor(Date.now() / 1000) + 86400, // 24 hours
    })
  )
  const message = `${header}.${payload}`
  const signature = sign(message, AUTH_SECRET)

  return `${message}.${signature}`
}
