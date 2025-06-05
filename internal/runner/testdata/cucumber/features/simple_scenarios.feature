Feature: Simple Scenarios
  Background:
    Given a background step

  Scenario: First simple scenario
    # Line 5
    Given a step
    When another step
    Then a final step

  Scenario: Second simple scenario
    # Line 10
    Given a different step

  Scenario: A pending scenario
    # Line 15
    Given a step that marks as pending

  Scenario: A skipped scenario
    # Line 19
    Given a step that skips

  Scenario: A failing scenario
    # Line 23
    Given a step that fails
