import NextAuth from 'next-auth'
import Google from 'next-auth/providers/google'
import MicrosoftEntraID from 'next-auth/providers/microsoft-entra-id'
import { PrismaAdapter } from '@auth/prisma-adapter'
import type { UserRole } from '@prisma/client'
import { db } from '@/lib/db'

const adminEmails = new Set(
  (process.env.ADMIN_EMAILS ?? '')
    .split(',')
    .map((s) => s.trim().toLowerCase())
    .filter(Boolean),
)

export const { handlers, auth, signIn, signOut } = NextAuth({
  adapter: PrismaAdapter(db),
  session: { strategy: 'jwt' },
  providers: [
    Google,
    MicrosoftEntraID({
      issuer:
        process.env.AUTH_MICROSOFT_ENTRA_ID_ISSUER ??
        'https://login.microsoftonline.com/common/v2.0',
    }),
  ],
  pages: {
    signIn: '/sign-in',
  },
  callbacks: {
    async jwt({ token, user }) {
      if (user?.id && user.email) {
        const shouldBeAdmin = adminEmails.has(user.email.toLowerCase())
        const desiredRole: UserRole = shouldBeAdmin ? 'ADMIN' : 'USER'
        const updated = await db.user.update({
          where: { id: user.id },
          data: { role: desiredRole },
          select: { role: true },
        })
        token.userId = user.id
        token.role = updated.role
      }
      return token
    },
    async session({ session, token }) {
      if (session.user) {
        session.user.id = (token.userId as string | undefined) ?? token.sub ?? ''
        session.user.role = (token.role as UserRole | undefined) ?? 'USER'
      }
      return session
    },
  },
})
