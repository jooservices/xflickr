#!/usr/bin/env python3
"""Exercise the PR guard with real Git history and representative descriptions."""

from __future__ import annotations

import importlib.util
import json
import os
import runpy
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
WORKSPACE = next(path for path in ROOT.parents if (path / 'scripts/dev/develop').is_file())
SPEC = importlib.util.spec_from_file_location('xflickr_pr_guard', ROOT / '.github/pr_guard.py')
GUARD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GUARD)
PROFILE = json.loads((ROOT / '.ai/project.json').read_text(encoding='utf-8'))
TEMPLATE = (ROOT / '.github/pull_request_template.md').read_text(encoding='utf-8')
TITLE = 'test(flow): Exercise the guard (JOOXF-518)'
BRANCH = 'JOOXF-518-test'


def description() -> str:
    sections = ['Jira', 'Requirement', 'What', 'Why', 'Out of scope', 'Risk / decisions', 'Tests', 'Verification']
    return '\n\n'.join(f'## {section}\n\n' + ('Ticket: [JOOXF-518](https://jooservices.atlassian.net/browse/JOOXF-518)' if section == 'Jira' else 'Verified.') for section in sections)


class PRGuardTests(unittest.TestCase):
    def test_AC5_pr_guard_enforces_the_template(self) -> None:
        valid = description()
        check = lambda body: GUARD.check_pull_request(PROFILE, TEMPLATE, TITLE, body, BRANCH)
        self.assertEqual(check(valid), [])
        cases = [
            (valid.replace('## Tests\n\nVerified.\n\n', ''), 'sections'),
            (valid.replace('## Tests\n\nVerified.', '## Tests\n\n'), 'empty'),
            (valid.replace('## Tests', '## Testing'), 'sections'),
            (valid.replace('JOOXF-518](https', 'JOOXF-000](https'), 'not a real ticket'),
            (valid.replace('JOOXF-518', 'JOOXF-519'), 'not in the'),
            (valid.replace('/browse/JOOXF-518)', '/browse/JOOXF-519)'), 'ticket link'),
            (valid.replace('## What\n\nVerified.', '## What\n\nGenerated with a tool'), 'forbidden trace'),
        ]
        for body, expected in cases:
            with self.subTest(expected=expected):
                self.assertTrue(any(expected in item for item in check(body)))
        altered_template = TEMPLATE.replace('## Tests', '## Testing')
        altered_body = valid.replace('## Tests', '## Testing')
        self.assertTrue(GUARD.check_pull_request(PROFILE, altered_template, TITLE, altered_body, BRANCH))
        self.assertTrue(GUARD.check_pull_request(PROFILE, TEMPLATE, 'bad title', valid, BRANCH))
        with tempfile.TemporaryDirectory(prefix='xflickr-pr-guard-', dir=WORKSPACE / '.tmp') as directory:
            repo = Path(directory)
            env = {**os.environ, 'GIT_CONFIG_GLOBAL': os.devnull, 'GIT_CONFIG_NOSYSTEM': '1',
                   'GIT_AUTHOR_NAME': 'Viet Vu', 'GIT_AUTHOR_EMAIL': 'jooservices@gmail.com',
                   'GIT_COMMITTER_NAME': 'Viet Vu', 'GIT_COMMITTER_EMAIL': 'jooservices@gmail.com'}
            subprocess.run(['git', 'init', '-q', '-b', 'main'], cwd=repo, env=env, check=True)
            subprocess.run(['git', 'commit', '-q', '--allow-empty', '-m', TITLE], cwd=repo, env=env, check=True)
            self.assertEqual(GUARD.check_commits(PROFILE, 'HEAD', repo), [])
            env['GIT_AUTHOR_NAME'] = 'Other'
            subprocess.run(['git', 'commit', '-q', '--allow-empty', '-m', TITLE], cwd=repo, env=env, check=True)
            self.assertTrue(any('author is' in item for item in GUARD.check_commits(PROFILE, 'HEAD~1..HEAD', repo)))
            env['GIT_AUTHOR_NAME'] = 'Viet Vu'
            env['GIT_COMMITTER_NAME'] = 'Other'
            subprocess.run(['git', 'commit', '-q', '--allow-empty', '-m', TITLE], cwd=repo, env=env, check=True)
            self.assertTrue(any('committer is' in item for item in GUARD.check_commits(PROFILE, 'HEAD~1..HEAD', repo)))
            env['GIT_COMMITTER_NAME'] = 'Viet Vu'
            subprocess.run(['git', 'commit', '-q', '--allow-empty', '-m', TITLE, '-m', 'Generated with a tool'], cwd=repo, env=env, check=True)
            self.assertTrue(any('forbidden trace' in item for item in GUARD.check_commits(PROFILE, 'HEAD~1..HEAD', repo)))

    def test_guard_reports_missing_ticket_and_ignores_intro(self) -> None:
        self.assertEqual(GUARD.sections('introduction\n## Jira\nTicket'), {'## Jira': 'Ticket'})
        self.assertIn('needs a line', GUARD.ticket_problems(PROFILE, '## Jira\nNo ticket', TITLE, BRANCH)[0])

    def test_guard_sync_copies_and_checks_the_project_files(self) -> None:
        with tempfile.TemporaryDirectory(prefix='xflickr-pr-sync-', dir=WORKSPACE / '.tmp') as directory:
            project = Path(directory)
            self.assertEqual(len(GUARD.sync(project, True)), 2)
            self.assertEqual(len(GUARD.sync(project, False)), 2)
            self.assertEqual(GUARD.sync(project, True), [])
            (project / '.github/pull_request_template.md').write_text('changed', encoding='utf-8')
            self.assertEqual(len(GUARD.sync(project, True)), 1)

    def test_guard_cli_reports_pass_and_failure(self) -> None:
        with tempfile.TemporaryDirectory(prefix='xflickr-pr-cli-', dir=WORKSPACE / '.tmp') as directory:
            path = Path(directory)
            event = path / 'event.json'
            event.write_text(json.dumps({'pull_request': {'title': TITLE, 'body': description(), 'head': {'ref': BRANCH}}}), encoding='utf-8')
            args = ['check', '--event', str(event), '--profile', str(ROOT / '.ai/project.json'), '--template', str(ROOT / '.github/pull_request_template.md')]
            self.assertEqual(GUARD.main(args), 0)
            env = {**os.environ, 'GIT_CONFIG_GLOBAL': os.devnull, 'GIT_CONFIG_NOSYSTEM': '1',
                   'GIT_AUTHOR_NAME': 'Viet Vu', 'GIT_AUTHOR_EMAIL': 'jooservices@gmail.com',
                   'GIT_COMMITTER_NAME': 'Viet Vu', 'GIT_COMMITTER_EMAIL': 'jooservices@gmail.com'}
            subprocess.run(['git', 'init', '-q', '-b', 'main'], cwd=path, env=env, check=True)
            subprocess.run(['git', 'commit', '-q', '--allow-empty', '-m', TITLE], cwd=path, env=env, check=True)
            self.assertEqual(GUARD.main([*args, '--commits', 'HEAD', '--repo', str(path)]), 0)
            event.write_text(json.dumps({'pull_request': {'title': 'bad', 'body': '', 'head': {'ref': BRANCH}}}), encoding='utf-8')
            self.assertEqual(GUARD.main(args), 1)
            self.assertEqual(GUARD.main(['sync', str(path), '--check']), 1)
            self.assertEqual(GUARD.main(['sync', str(path)]), 0)
            self.assertEqual(GUARD.main(['sync', str(path), '--check']), 0)
            with mock.patch.object(sys, 'argv', [str(ROOT / '.github/pr_guard.py'), *args]):
                with self.assertRaises(SystemExit) as raised:
                    runpy.run_path(str(ROOT / '.github/pr_guard.py'), run_name='__main__')
                self.assertEqual(raised.exception.code, 1)


if __name__ == '__main__':
    unittest.main()
