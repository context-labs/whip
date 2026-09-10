"""Exercise real Python worker overlap, never native evaluation jobs."""
import threading
import time
import unittest

from whip_evals.execution import run_pool


class PoolTests(unittest.TestCase):
    def trials(self, count):
        return [dict(id=str(i), task_id=str(i), resources=dict(cpus=2, memory_mb=8, storage_mb=20)) for i in range(count)]

    def test_admission_reserves_whole_trial_and_obeys_max_jobs(self):
        for jobs, capacity, expected in [(32, dict(cpus=6, memory_mb=16, storage_mb=100), 2),
                                         (1, dict(cpus=100, memory_mb=100, storage_mb=1000), 1)]:
            active = peak = 0
            lock = threading.Lock()
            def worker(trial, cancelled):
                nonlocal active, peak
                with lock:
                    active += 1
                    peak = max(active, peak)
                time.sleep(.03)
                with lock:
                    active -= 1
                return {'started': True, 'cleanup': {'complete': True}}
            stats = {}
            results = run_pool(self.trials(8), capacity, jobs, worker, stats=stats)
            self.assertEqual(peak, expected)
            self.assertEqual(stats['peak_active_trials'], expected)
            self.assertEqual(len(results), 8)
            self.assertTrue(all(r['phase_seconds']['queue'] >= 0 for r in results.values()))

    def test_cleanup_failure_halts_new_dispatch(self):
        started = []
        def worker(trial, cancelled):
            started.append(trial['id'])
            return {'started': True, 'cleanup': {'complete': False}}
        results = run_pool(self.trials(3), dict(cpus=2, memory_mb=8, storage_mb=20), 32, worker)
        self.assertEqual(started, ['0'])
        self.assertFalse(results['1']['started'])
        self.assertTrue(results['1']['cancelled'])

    def test_callback_exception_cancels_active_workers_before_shutdown(self):
        event = threading.Event()
        overlap = threading.Event()
        stopped = threading.Event()
        def worker(trial, cancelled):
            if trial['id'] == '0':
                self.assertTrue(overlap.wait(2))
            else:
                overlap.set()
                if cancelled.wait(2):
                    stopped.set()
            return {'cleanup': {'complete': True}}
        def callback(*_):
            raise RuntimeError('offline disk failure')
        start = time.monotonic()
        with self.assertRaisesRegex(RuntimeError, 'disk failure'):
            run_pool(self.trials(2), dict(cpus=4, memory_mb=16, storage_mb=40), 2, worker,
                     cancelled=event, on_result=callback)
        self.assertTrue(event.is_set())
        self.assertTrue(stopped.is_set())
        self.assertLess(time.monotonic() - start, 1.5)

    def test_impossible_task_fails_before_dispatch(self):
        with self.assertRaises(ValueError):
            run_pool(self.trials(1), dict(cpus=1, memory_mb=8, storage_mb=20), 32,
                     lambda *_: self.fail('worker must not start'))
