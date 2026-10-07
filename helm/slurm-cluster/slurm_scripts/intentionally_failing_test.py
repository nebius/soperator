import unittest


class IntentionallyFailingTest(unittest.TestCase):
    def test_ci_reports_python_failures(self):
        self.fail("intentional failure to verify the test-python CI job turns red")


if __name__ == "__main__":
    unittest.main(verbosity=2)
