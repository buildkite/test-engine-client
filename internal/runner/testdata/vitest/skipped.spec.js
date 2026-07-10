import { describe, it } from 'vitest'

describe('this will be skipped', () => {
  it.skip('for sure', () => {})
  it.todo('todo yeah')
})
