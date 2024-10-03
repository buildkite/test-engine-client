RSpec.describe("Spec with skipped examples") do
  it("not skipped") do
    true
  end

  xit("skiped using xit") do
    fail
  end

  it("skipped using skip option", skip: "skipped") do
    fail
  end

  it("skipped using pending option", pending: "skipped") do
    fail
  end
end
