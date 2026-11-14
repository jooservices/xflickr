#!/usr/bin/env python3
"""Exercise the XFlickr profile through the workspace developer tools in an isolated Git fixture."""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKSPACE = next(path for path in ROOT.parents if (path / 'scripts/dev/develop').is_file())


def run(command: list[str], cwd: Path, **kwargs) -> subprocess.CompletedProcess:
    env = {**os.environ, 'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': os.devnull}
    env.update(kwargs.pop('env', {}))
    return subprocess.run(command, cwd=cwd, env=env, text=True, capture_output=True, check=False, **kwargs)


class HooksTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix='xflickr-hooks-', dir=WORKSPACE / '.tmp')
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name) / 'repo'
        self.repo.mkdir()
        self.git('init', '-q', '-b', 'develop')
        self.git('config', 'user.name', 'Viet Vu')
        self.git('config', 'user.email', 'jooservices@gmail.com')
        data = json.loads((ROOT / '.ai/project.json').read_text(encoding='utf-8'))
        data['setup'] = []
        data['stages'] = [{**data['stages'][0], 'name': 'fixture', 'cmd': 'true', 'cwd': '.', 'when': [], 'changedCmd': 'true'}]
        (self.repo / '.ai').mkdir()
        (self.repo / '.ai/project.json').write_text(json.dumps(data), encoding='utf-8')
        self.git('add', '.ai/project.json')
        self.git('commit', '-q', '-m', 'chore(test): Create fixture (JOOXF-518)')
        self.git('update-ref', 'refs/remotes/origin/develop', self.git('rev-parse', 'HEAD').stdout.strip())
        result = run([str(WORKSPACE / 'scripts/dev/develop'), 'JOOXF-518', '--repo', str(self.repo), '--company', 'JOOservices', '--description', 'test'], WORKSPACE)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.worktree = Path(json.loads(result.stdout)['worktree'])

    def git(self, *args: str, cwd: Path | None = None, **kwargs) -> subprocess.CompletedProcess:
        result = run(['git', *args], cwd or self.repo, **kwargs)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result

    def test_AC2_develop_installs_the_hooks_and_the_identity(self) -> None:
        hooks = Path(self.git('config', '--get', 'core.hooksPath', cwd=self.worktree).stdout.strip())
        self.assertEqual(hooks, self.repo / '.githooks')
        self.assertEqual({item.name for item in hooks.iterdir()}, {'commit-msg', 'pre-commit', 'pre-push'})
        self.assertEqual(self.git('config', '--get', 'core.hooksPath').stdout.strip(), str(hooks))
        self.assertEqual(self.git('config', '--get', 'user.name', cwd=self.worktree).stdout.strip(), 'Viet Vu')
        self.assertEqual(self.git('config', '--get', 'user.email', cwd=self.worktree).stdout.strip(), 'jooservices@gmail.com')

    def test_AC3_hooks_refuse_what_must_not_be_committed_or_pushed(self) -> None:
        good = 'test(flow): Check the hooks (JOOXF-518)'
        cases = [
            (['-m', 'bad header'], {}),
            (['-m', 'test(flow): ' + 'A' * 121 + ' (JOOXF-518)'], {}),
            (['-m', 'test(flow): Wrong key (JOOXF-519)'], {}),
            (['-m', good, '-m', 'Generated with a tool'], {}),
            (['-m', good], {'GIT_AUTHOR_NAME': 'Other'}),
            (['-m', good], {'GIT_COMMITTER_NAME': 'Other'}),
        ]
        for args, env in cases:
            with self.subTest(args=args, env=env):
                result = run(['git', 'commit', '--allow-empty', *args], self.worktree, env=env)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.git('commit', '--allow-empty', '-m', 'test(flow): X (JOOXF-518)', cwd=self.worktree)
        prefix, suffix = 'test(flow): ', ' (JOOXF-518)'
        self.git('commit', '--allow-empty', '-m', prefix + 'A' * (120 - len(prefix) - len(suffix)) + suffix, cwd=self.worktree)
        self.git('commit', '--allow-empty', '-m', good, cwd=self.worktree)
        remote = Path(self.temp.name) / 'remote.git'
        self.git('init', '--bare', '-q', str(remote))
        self.git('remote', 'add', 'local', str(remote))
        push = ['git', 'push', 'local', 'JOOXF-518-test:JOOXF-518-test']
        denied = run(push, self.worktree)
        self.assertNotEqual(denied.returncode, 0)
        self.assertIn('proof', denied.stderr)
        check = run([str(WORKSPACE / 'scripts/dev/check'), 'final'], self.worktree)
        self.assertEqual(check.returncode, 0, check.stdout + check.stderr)
        allowed = run(push, self.worktree)
        self.assertEqual(allowed.returncode, 0, allowed.stderr)

    def test_AC4_check_lists_the_stages_and_runs_one(self) -> None:
        listed = run([str(WORKSPACE / 'scripts/dev/check'), 'list'], ROOT)
        self.assertEqual(listed.returncode, 0, listed.stderr)
        self.assertEqual(len(listed.stdout.splitlines()), 17)
        check = run([str(WORKSPACE / 'scripts/dev/check'), 'run', 'secrets', '--all'], ROOT)
        self.assertEqual(check.returncode, 0, check.stdout + check.stderr)


if __name__ == '__main__':
    unittest.main()
