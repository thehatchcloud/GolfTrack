import type { Metadata, Viewport } from 'next'
import { Geist, Geist_Mono } from 'next/font/google'
import { headers } from 'next/headers'
import './globals.css'

const geistSans = Geist({
  variable: '--font-geist-sans',
  subsets: ['latin'],
})

const geistMono = Geist_Mono({
  variable: '--font-geist-mono',
  subsets: ['latin'],
})

export const metadata: Metadata = {
  title: 'Golf Track',
  description: 'Mobile-first golf score tracking for courses, rounds, and shot-by-shot scoring.',
}

export const viewport: Viewport = {
  themeColor: '#047857',
}

export default async function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode
}>) {
  const nonce = (await headers()).get('x-nonce') ?? undefined
  return (
    <html lang="en" className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`} nonce={nonce}>
      <body className="min-h-full flex flex-col">{children}</body>
    </html>
  )
}
