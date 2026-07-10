import { describe, it, expect } from 'vitest'

describe('this will fail', () => {
  it('for sure', () => {
    expect(true).toBe(false)
  })
})
