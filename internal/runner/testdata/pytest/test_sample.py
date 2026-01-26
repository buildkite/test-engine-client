import pytest

@pytest.mark.execution_tag("priority", "high")
@pytest.mark.execution_tag("team", "frontend")
def test_happy():
    assert 3 == 3
