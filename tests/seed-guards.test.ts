import test from 'node:test'
import assert from 'node:assert/strict'

import { checkSeedGuards } from '../prisma/seed'

function withEnv(overrides: Record<string, string | undefined>, fn: () => void) {
  const saved: Record<string, string | undefined> = {}
  for (const key of Object.keys(overrides)) {
    saved[key] = process.env[key]
    if (overrides[key] === undefined) {
      delete process.env[key]
    } else {
      process.env[key] = overrides[key]
    }
  }
  try {
    fn()
  } finally {
    for (const key of Object.keys(saved)) {
      if (saved[key] === undefined) {
        delete process.env[key]
      } else {
        process.env[key] = saved[key]
      }
    }
  }
}

test('throws when NODE_ENV is production', () => {
  withEnv({ NODE_ENV: 'production', DATABASE_URL: 'file:./prisma/dev.db' }, () => {
    assert.throws(() => checkSeedGuards(), /Refusing to seed in production/)
  })
})

test('throws when DATABASE_URL points at a production path', () => {
  withEnv({ NODE_ENV: 'development', DATABASE_URL: 'file:/data/prod.db' }, () => {
    assert.throws(
      () => checkSeedGuards(),
      /Refusing to seed: DATABASE_URL does not look like a dev DB/,
    )
  })
})

test('throws when DATABASE_URL is unset', () => {
  withEnv({ NODE_ENV: 'development', DATABASE_URL: undefined }, () => {
    assert.throws(
      () => checkSeedGuards(),
      /Refusing to seed: DATABASE_URL does not look like a dev DB/,
    )
  })
})

test('passes when DATABASE_URL contains dev.db', () => {
  withEnv({ NODE_ENV: 'development', DATABASE_URL: 'file:./prisma/dev.db' }, () => {
    assert.doesNotThrow(() => checkSeedGuards())
  })
})

test('passes when DATABASE_URL contains test.db', () => {
  withEnv({ NODE_ENV: 'test', DATABASE_URL: 'file:./prisma/test.db' }, () => {
    assert.doesNotThrow(() => checkSeedGuards())
  })
})
