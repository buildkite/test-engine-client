RSpec.describe "Tomato" do
  it "is red" do
    true
  end

  it "is vegetable" do
    if ENV["RETRY"] == "true"
      expect(true).to eq(true)
    else
      expect(true).to eq(false)
    end
  end
end
