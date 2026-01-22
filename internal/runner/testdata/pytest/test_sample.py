import pytest

@pytest.mark.test_execution("key", "value")
def test_happy():
    assert 3 == 3
