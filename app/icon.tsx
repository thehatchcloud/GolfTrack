import { ImageResponse } from 'next/og'

export const size = { width: 32, height: 32 }
export const contentType = 'image/png'

export default function Icon() {
  return new ImageResponse(
    (
      <div
        style={{
          width: 32,
          height: 32,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          background: '#047857',
          borderRadius: 6,
        }}
      >
        <svg width={20} height={20} viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg">
          <rect x="38" y="18" width="4" height="58" fill="white" rx="2" />
          <polygon points="42,18 42,46 78,32" fill="white" />
          <ellipse cx="40" cy="76" rx="22" ry="6" fill="rgba(255,255,255,0.35)" />
        </svg>
      </div>
    ),
    { ...size }
  )
}
