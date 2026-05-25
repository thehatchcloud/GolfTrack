// Simple test token - in tests, we mock getToken to return a token with this structure
export function createAuthToken(userId: string): string {
  // Return a simple string that we'll use to identify the user in mocked getToken
  return `test-token-${userId}`
}
