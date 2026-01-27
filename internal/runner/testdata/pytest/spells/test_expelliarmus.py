import pytest

class TestExpelliarmus:
    @pytest.mark.execution_tag("team", "backend")
    def test_disarms_opponent(self):
        assert True

    @pytest.mark.execution_tag("team", "frontend")
    def test_knocks_wand_out(self):
        assert True
