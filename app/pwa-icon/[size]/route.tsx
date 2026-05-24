import { ImageResponse } from 'next/og'
import type { NextRequest } from 'next/server'

export async function GET(
  _req: NextRequest,
  { params }: { params: Promise<{ size: string }> }
) {
  const { size } = await params
  const dim = Number(size)

  if (!Number.isFinite(dim) || dim < 16 || dim > 1024) {
    return new Response('Invalid size', { status: 400 })
  }

  const pad = Math.round(dim * 0.15)

  return new ImageResponse(
    (
      <div
        style={{
          width: dim,
          height: dim,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          background: '#047857',
        }}
      >
        <svg
          width={dim - pad * 2}
          height={dim - pad * 2}
          viewBox="0 0 100 100"
          xmlns="http://www.w3.org/2000/svg"
        >
          <rect x="38" y="18" width="4" height="58" fill="white" rx="2" />
          <polygon points="42,18 42,46 78,32" fill="white" />
          <ellipse cx="40" cy="76" rx="22" ry="6" fill="rgba(255,255,255,0.35)" />
        </svg>
      </div>
    ),
    { width: dim, height: dim, headers: { 'Cache-Control': 'public, max-age=31536000, immutable' } }
  )
}
