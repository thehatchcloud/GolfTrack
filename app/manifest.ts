import type { MetadataRoute } from 'next'

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: 'Golf Track',
    short_name: 'Golf Track',
    description: 'Mobile-first golf score tracking for courses, rounds, and shot-by-shot scoring.',
    start_url: '/',
    display: 'standalone',
    background_color: '#ffffff',
    theme_color: '#047857',
    icons: [
      { src: '/pwa-icon/192', sizes: '192x192', type: 'image/png' },
      { src: '/pwa-icon/512', sizes: '512x512', type: 'image/png' },
      { src: '/pwa-icon/512', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
    ],
  }
}
