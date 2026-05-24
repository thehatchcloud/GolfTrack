import Link from 'next/link'
import { auth, signOut } from '@/auth'

export async function UserNav() {
  const session = await auth()

  if (!session?.user) {
    return (
      <Link
        href="/sign-in"
        className="rounded-full bg-emerald-700 px-3 py-2 text-sm font-medium text-white transition hover:bg-emerald-800 active:bg-emerald-900"
      >
        Sign in
      </Link>
    )
  }

  const label = session.user.name ?? session.user.email ?? 'Account'

  async function handleSignOut() {
    'use server'
    await signOut({ redirectTo: '/sign-in' })
  }

  return (
    <div className="flex items-center gap-2">
      <span className="hidden max-w-[10rem] truncate text-sm text-stone-600 sm:inline" title={label}>
        {label}
      </span>
      <form action={handleSignOut}>
        <button
          type="submit"
          className="rounded-full border border-stone-300 bg-white px-3 py-2 text-sm font-medium text-stone-700 transition hover:bg-stone-100 active:bg-stone-200"
        >
          Sign out
        </button>
      </form>
    </div>
  )
}
