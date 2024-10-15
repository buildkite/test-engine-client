describe("Flaky spec", () => {
  it("is 50% flaky", () => {
    expect(Math.random() > 0.5).to.be.true;
  });
});
