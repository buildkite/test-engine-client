describe('slow test', () => {
  it('wait for 2 seconds', async () => {
    await new Promise((resolve) => setTimeout(resolve, 2000));
  })
})
