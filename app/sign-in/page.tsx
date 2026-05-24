import { redirect } from 'next/navigation'
import { auth, signIn } from '@/auth'

type SearchParams = Promise<{ callbackUrl?: string; error?: string }>

export default async function SignInPage({ searchParams }: { searchParams: SearchParams }) {
  const session = await auth()
  const { callbackUrl, error } = await searchParams
  const redirectTo = callbackUrl && callbackUrl.startsWith('/') ? callbackUrl : '/'

  if (session?.user) {
    redirect(redirectTo)
  }

  async function signInWith(provider: 'google' | 'microsoft-entra-id') {
    'use server'
    await signIn(provider, { redirectTo })
  }

  return (
    <div className="mx-auto flex w-full max-w-sm flex-col items-center gap-6 py-12">
      <div className="flex flex-col items-center gap-2 text-center">
        <h1 className="text-2xl font-semibold tracking-tight text-stone-900">Sign in to Golf Track</h1>
        <p className="text-sm text-stone-600">Use your Google or Microsoft account to continue.</p>
      </div>
      {error ? (
        <p className="w-full rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
          Sign-in failed. Please try again.
        </p>
      ) : null}
      <div className="flex w-full flex-col gap-3">
        <form action={signInWith.bind(null, 'google')}>
          <button
            type="submit"
            className="w-full rounded-full border border-stone-300 bg-white px-4 py-2.5 text-sm font-medium text-stone-900 transition hover:bg-stone-50 active:bg-stone-100"
          >
            Continue with Google
          </button>
        </form>
        <form action={signInWith.bind(null, 'microsoft-entra-id')}>
          <button
            type="submit"
            className="w-full rounded-full border border-stone-300 bg-white px-4 py-2.5 text-sm font-medium text-stone-900 transition hover:bg-stone-50 active:bg-stone-100"
          >
            Continue with Microsoft
          </button>
        </form>
      </div>
    </div>
  )
}
