RSpec.describe "bad syntax" do
  it "is missing an end" do
    if true
      expect(true).to be_truthy
  end
end
