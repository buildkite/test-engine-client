Given("A functioning wand") do
  true
end

Then('the result should be {int}') do |int|
  expect(@result).to eq(int)
end

Given('a background step') do
  true
end

Given('a step') do
  true
end

When('another step') do
  true
end

Then('a final step') do
  true
end

Given('a different step') do
  true
end

Given('a step here') do
  true
end

Then("Opponent is disarmed") do
  true
end

Then("Something unexpected happens that causes a failure") do
  raise "This step is designed to fail!"
end

Given('a step that marks as pending') do
  pending "This step is intentionally pending."
end

Given('a step that skips') do
  skip_this_scenario("This scenario is intentionally skipped.")
end

Given('a step that fails') do
  raise "This step is designed to fail!"
end
