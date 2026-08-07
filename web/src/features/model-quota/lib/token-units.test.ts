import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import { formatTokensAsMillions, parseMillionsToTokens } from './token-units'

describe('parseMillionsToTokens', () => {
  it('converts M units without floating point rounding', () => {
    assert.equal(parseMillionsToTokens('1.25'), 1_250_000)
    assert.equal(parseMillionsToTokens('0.001'), 1_000)
  })

  it('rejects unsupported precision and numeric formats', () => {
    assert.equal(parseMillionsToTokens('1.0001'), null)
    assert.equal(parseMillionsToTokens('-1'), null)
    assert.equal(parseMillionsToTokens('1e2'), null)
    assert.equal(parseMillionsToTokens(''), null)
  })
})

describe('formatTokensAsMillions', () => {
  it('formats tokens as a compact M value', () => {
    assert.equal(formatTokensAsMillions(100_000_000), '100')
    assert.equal(formatTokensAsMillions(1_250_000), '1.25')
  })
})
