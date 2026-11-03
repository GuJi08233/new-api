import importlib.util
from pathlib import Path
import socket
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('site_check', Path(__file__).with_name('check.py'))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class IsolationTests(unittest.TestCase):
    def test_occupied_port_is_refused_before_any_http(self):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
            listener.bind(('127.0.0.1', 0))
            listener.listen()
            with self.assertRaisesRegex(RuntimeError, 'already occupied'):
                check.require_free_port(listener.getsockname()[1])

    def test_readiness_refuses_another_server_version(self):
        class OtherServer:
            def call(self, method, path):
                self.called = (method, path)
                return {'version': 'another-site'}
        client = OtherServer()
        with self.assertRaisesRegex(RuntimeError, 'another process or version'):
            check.wait_ready(client, 123, 'random-fixture-version', alive=lambda _: True)
        self.assertEqual(('GET', '/api/status'), client.called)

    def test_exited_new_process_never_contacts_http(self):
        class NeverCall:
            def call(self, *_):
                raise AssertionError('must not contact an existing listener')
        with self.assertRaisesRegex(RuntimeError, 'exited before readiness'):
            check.wait_ready(NeverCall(), 123, 'fixture', alive=lambda _: False)

    def test_failed_shutdown_preserves_tracking_and_retry_succeeds(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            stop = work / 'fixture.stop'
            tracked = [(123, stop, 'fixture')]
            with self.assertRaisesRegex(RuntimeError, 'remains tracked'):
                check.stop_tracked(tracked, work, alive=lambda _: True, attempts=1, pause=lambda _: None)
            self.assertEqual(1, len(tracked))
            self.assertTrue(stop.exists())
            (work / 'fixture.log').write_text('received signal\nserver exited\n', encoding='utf-8')
            check.stop_tracked(tracked, work, alive=lambda _: False, attempts=1)
            self.assertEqual([], tracked)


if __name__ == '__main__':
    unittest.main()
