describe('Passing spec', () => {
  beforeEach(() => {
    cy.visit('index.html')
  })

  it('has a title', () => {
    cy.title().should('eq', 'Buildkite Test Engine Client - Cypress Example')
  })

  it('says hello', () => {
    cy.contains('Hello there!')
  })
})
