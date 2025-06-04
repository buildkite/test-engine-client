Given("A functioning wand") do
  true
end

Then("Opponent is disarmed") do
  true
end

Then("Something unexpected happens that causes a failure") do
  raise "This step is designed to fail!"
end
