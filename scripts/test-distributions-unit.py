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
        self.ready = {'state': 'running', 'daemon_build': 'v1.0.0-alpha.10', 'pid': 2, 'generation': 2,
                      'gateway': {'state': 'ready', 'endpoint': 'http://127.0.0.1:1234'},
                      'network_endpoint': 'http://127.0.0.1:1234'}

    def test_new_build_does_not_imply_gateway_readiness(self):
        starting = {**self.ready, 'gateway': {'state': 'starting'}}
        del starting['network_endpoint']
        status = Mock(side_effect=[starting, self.ready])
        with patch.object(distribution.time, 'sleep'):
            result = distribution.wait_for_gateway(status, 'v1.0.0-alpha.10', previous_generation=1)
        self.assertEqual(result, self.ready)
        self.assertEqual(status.call_count, 2)

    def test_ready_gateway_without_network_endpoint_is_not_usable(self):
        missing_endpoint = dict(self.ready)
        del missing_endpoint['network_endpoint']
        status = Mock(side_effect=[missing_endpoint, self.ready])
        with patch.object(distribution.time, 'sleep'):
            result = distribution.wait_for_gateway(status, 'v1.0.0-alpha.10')
        self.assertEqual(result, self.ready)
        self.assertEqual(status.call_count, 2)

    def test_in_place_exec_can_be_ready_with_the_same_pid(self):
        # Automatic updates replace the daemon process image without a new PID.
        ready = {**self.ready, 'pid': 1}
        result = distribution.wait_for_gateway(lambda: ready, 'v1.0.0-alpha.10',
                                               previous_generation=1)
        self.assertEqual(result, ready)

    def test_unchanged_generation_or_wrong_build_does_not_complete_restart(self):
        status = Mock(side_effect=[{**self.ready, 'generation': 1},
                                   {**self.ready, 'daemon_build': 'v1.0.0-alpha.9'}, self.ready])
        with patch.object(distribution.time, 'sleep'):
            result = distribution.wait_for_gateway(status, 'v1.0.0-alpha.10', previous_generation=1)
        self.assertEqual(result, self.ready)
        self.assertEqual(status.call_count, 3)

    def test_failed_missing_or_inconsistent_gateway_times_out_with_status(self):
        for gateway in [{'state': 'failed', 'error': 'fixture bind failed'},
                        {'state': 'starting'},
                        {'state': 'ready', 'endpoint': 'http://127.0.0.1:9999'}]:
            with self.subTest(gateway=gateway):
                observed = {**self.ready, 'gateway': gateway}
                with patch.object(distribution.time, 'sleep'), \
                        patch.object(distribution.time, 'monotonic', side_effect=[0, 0, 2]):
                    with self.assertRaises(AssertionError) as error:
                        distribution.wait_for_gateway(lambda: observed, 'v1.0.0-alpha.10', timeout=1)
                self.assertIn(str(observed), str(error.exception))


if __name__ == '__main__':
    unittest.main()
