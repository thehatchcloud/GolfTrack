import Link from 'next/link'
import type { ReactNode } from 'react'

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-stone-50 text-stone-950">
      <header className="sticky top-0 z-30 border-b border-stone-200 bg-white/95 backdrop-blur supports-[backdrop-filter]:bg-white/85">
        <div className="mx-auto flex max-w-4xl items-center justify-between px-4 py-3 sm:py-4">
          <Link href="/" className="text-lg font-semibold tracking-tight text-emerald-700">
            Golf Track
          </Link>
          <nav className="flex items-center gap-1 text-sm font-medium sm:gap-2">
            <Link
              href="/courses"
              className="rounded-full px-3 py-2 text-stone-700 transition hover:bg-stone-100 active:bg-stone-200"
            >
              Courses
            </Link>
            <Link
              href="/rounds"
              className="rounded-full px-3 py-2 text-stone-700 transition hover:bg-stone-100 active:bg-stone-200"
            >
              Rounds
            </Link>
          </nav>
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-4xl flex-1 flex-col px-4 py-5 sm:py-6">{children}</main>
    </div>
  )
}
