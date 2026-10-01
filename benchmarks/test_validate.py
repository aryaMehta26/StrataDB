import copy
import json
from pathlib import Path
import tempfile
import unittest
from validate import load_results


class ValidationTests(unittest.TestCase):
    def setUp(self):
        self.rows = load_results(Path(__file__).with_name("results.jsonl"))

    def check_invalid(self, rows):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "results.jsonl"
            path.write_text("\n".join(json.dumps(row) for row in rows))
            with self.assertRaises(ValueError):
                load_results(path)

    def test_duplicate_does_not_hide_missing_configuration(self):
        self.check_invalid(self.rows[:-1] + [self.rows[0]])

    def test_rejects_mixed_workload_sizes(self):
        rows = copy.deepcopy(self.rows)
        rows[0]["keys"] += 1
        self.check_invalid(rows)

    def test_rejects_nonfinite_latency(self):
        rows = copy.deepcopy(self.rows)
        rows[0]["p99_us"] = float("nan")
        self.check_invalid(rows)

    def test_rejects_inconsistent_throughput(self):
        rows = copy.deepcopy(self.rows)
        rows[0]["ops_per_second"] *= 2
        self.check_invalid(rows)


if __name__ == "__main__":
    unittest.main()
