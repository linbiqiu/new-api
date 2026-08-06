const TOKENS_PER_MILLION = 1_000_000n

export function parseMillionsToTokens(value: string): number | null {
  const normalized = value.trim()
  if (!/^\d+(?:\.\d{1,3})?$/.test(normalized)) return null

  const [wholePart, decimalPart = ''] = normalized.split('.')
  const tokens =
    BigInt(wholePart) * TOKENS_PER_MILLION + BigInt(decimalPart.padEnd(6, '0'))

  if (tokens > BigInt(Number.MAX_SAFE_INTEGER)) return null
  return Number(tokens)
}

export function formatTokensAsMillions(tokens: number): string {
  if (!Number.isSafeInteger(tokens) || tokens < 0) return '0'

  const value = BigInt(tokens)
  const whole = value / TOKENS_PER_MILLION
  const remainder = value % TOKENS_PER_MILLION
  if (remainder === 0n) return whole.toString()

  const decimal = remainder.toString().padStart(6, '0').replace(/0+$/, '')
  return `${whole}.${decimal}`
}
