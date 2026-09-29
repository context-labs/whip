#!/usr/bin/env python3
"""Deterministic regressions for packaged acceptance's asynchronous readiness."""
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location('distribution', Path(__file__).with_name('test-distributions.py'))
distribution = importlib.util.module_from_spec(spec)
spec.loader.exec_module(distribution)


class GatewayReadinessTests(unittest.TestCase):
    def setUp(self):
        self.ready = {'state': 'running', 'process': {
            'build': 'v1.0.0-alpha.10', 'pid': 2, 'runtime_id': 'owned-runtime',
            'process_epoch': 'new-process', 'web_state': 'running',
            'web_endpoint': 'http://127.0.0.1:1234'}}
        self.previous = {**self.ready['process'], 'pid': 1, 'process_epoch': 'old-process'}

    def test_new_build_does_not_imply_gateway_readiness(self):
        starting = {**self.ready, 'process': {**self.ready['process'], 'web_state': 'starting'}}
        del starting['process']['web_endpoint']
        status = Mock(side_effect=[starting, self.ready])
        with patch.object(distribution.time, 'sleep'):
            result = distribution.wait_for_gateway(status, 'v1.0.0-alpha.10', previous_process=self.previous)
        self.assertEqual(result, self.ready)
        self.assertEqual(status.call_count, 2)

    def test_ready_gateway_without_endpoint_is_not_usable(self):
        missing_endpoint = {**self.ready, 'process': dict(self.ready['process'])}
        del missing_endpoint['process']['web_endpoint']
        status = Mock(side_effect=[missing_endpoint, self.ready])
        with patch.object(distribution.time, 'sleep'):
            result = distribution.wait_for_gateway(status, 'v1.0.0-alpha.10')
        self.assertEqual(result, self.ready)
        self.assertEqual(status.call_count, 2)

    def test_new_epoch_can_be_ready_with_the_same_pid(self):
        # PID reuse cannot replace the verified runtime/epoch comparison.
        ready = {**self.ready, 'process': {**self.ready['process'], 'pid': 1}}
        result = distribution.wait_for_gateway(lambda: ready, 'v1.0.0-alpha.10',
                                               previous_process=self.previous)
        self.assertEqual(result, ready)

    def test_unchanged_epoch_wrong_runtime_or_wrong_build_does_not_complete_restart(self):
        status = Mock(side_effect=[
            {**self.ready, 'process': {**self.ready['process'], **change}}
            for change in [{'process_epoch': 'old-process'}, {'runtime_id': 'unrelated-runtime'},
                           {'build': 'v1.0.0-alpha.9'}]] + [self.ready])
        with patch.object(distribution.time, 'sleep'):
            result = distribution.wait_for_gateway(status, 'v1.0.0-alpha.10', previous_process=self.previous)
        self.assertEqual(result, self.ready)
        self.assertEqual(status.call_count, 4)

    def test_failed_missing_or_inconsistent_gateway_times_out_with_status(self):
        for change in [{'web_state': 'failed', 'web_error': 'fixture bind failed'},
                       {'web_state': 'starting'}, {'web_endpoint': None},
                       {'runtime_id': ''}, {'process_epoch': None}]:
            with self.subTest(change=change):
                observed = {**self.ready, 'process': {**self.ready['process'], **change}}
                with patch.object(distribution.time, 'sleep'), \
                        patch.object(distribution.time, 'monotonic', side_effect=[0, 0, 2]):
                    with self.assertRaises(AssertionError) as error:
                        distribution.wait_for_gateway(lambda: observed, 'v1.0.0-alpha.10', timeout=1)
                self.assertIn(str(observed), str(error.exception))


if __name__ == '__main__':
    unittest.main()
