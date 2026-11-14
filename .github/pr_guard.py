"""PR guard: a pull request follows the one template, links its Jira ticket and carries no AI trace.

Standard library only, so a project's CI can run a copy of this file (`sync` puts the copy and the template in a
project). The rules come from the project's own `.ai/project.json`: ticketKeyRegex, commitMessage.regex and
identity (name, email, forbiddenTraces).
"""

from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
TEMPLATE_NAME = "pull_request_template.md"
GUARD_NAME = "pr_guard.py"
COMMENT = re.compile(r"<!--.*?-->", re.DOTALL)
PLACEHOLDER_KEY = re.compile(r"\b[A-Z][A-Z0-9]*-0+\b")
TICKET_LINE = re.compile(r"^Ticket:\s*\[([^\]]+)\]\((https?://[^)\s]+)\)\s*$", re.MULTILINE)


def headings(template: str) -> list[str]:
    return [line for line in template.splitlines() if line.startswith("## ")]


def sections(body: str) -> dict[str, str]:
    """The text under each `## ` heading, in order; comments removed."""
    found: dict[str, list[str]] = {}
    current: str | None = None
    for line in COMMENT.sub("", body.replace("\r\n", "\n")).splitlines():
        if line.startswith("## "):
            current = line.rstrip()
            found.setdefault(current, [])
        elif current is not None:
            found[current].append(line)
    return {heading: "\n".join(lines).strip() for heading, lines in found.items()}


def structure_problems(template: str, body: str) -> list[str]:
    wanted = headings(template)
    present = [line.rstrip() for line in body.replace("\r\n", "\n").splitlines() if line.startswith("## ")]
    if present != wanted:
        return [f"the description must have exactly these sections in this order: {', '.join(item[3:] for item in wanted)}"]
    return [f"the section '{heading[3:]}' is empty" for heading, text in sections(body).items() if not text]


def ticket_problems(profile: dict, body: str, title: str, branch: str) -> list[str]:
    match = TICKET_LINE.search(sections(body).get("## Jira", ""))
    if match is None:
        return ["the Jira section needs a line `Ticket: [KEY](https://...)`"]
    key = match.group(1)
    pattern = profile["ticketKeyRegex"]
    if not re.fullmatch(pattern, key) or PLACEHOLDER_KEY.fullmatch(key):
        return [f"'{key}' is not a real ticket key"]
    return [f"the ticket {key} is not in the {where}" for where, text in (("PR title", title), ("branch name", branch)) if key not in re.findall(pattern, text)]


def trace_problems(profile: dict, texts: dict[str, str]) -> list[str]:
    problems = []
    for where, text in texts.items():
        for pattern in profile["identity"]["forbiddenTraces"]:
            found = re.search(pattern, text)
            if found:
                problems.append(f"{where} carries a forbidden trace ({found.group(0).strip()!r}); it must not mention AI")
    return problems


def check_pull_request(profile: dict, template: str, title: str, body: str, branch: str) -> list[str]:
    problems = [] if re.fullmatch(profile["commitMessage"]["regex"], title) else [f"the PR title does not match the commit format: {title}"]
    problems += structure_problems(template, body) + ticket_problems(profile, body, title, branch)
    return problems + trace_problems(profile, {"the PR title": title, "the PR description": body})


def check_commits(profile: dict, revisions: str, repo: Path) -> list[str]:
    """Every commit of the range has the project's author and committer and a message without a trace."""
    identity = profile["identity"]
    result = subprocess.run(["git", "-C", str(repo), "log", "--format=%H%x00%an%x00%ae%x00%cn%x00%ce%x00%B%x01", revisions], capture_output=True, text=True, check=True)
    problems = []
    for record in (item.strip("\n") for item in result.stdout.split("\x01") if item.strip("\n")):
        sha, an, ae, cn, ce, message = record.split("\0", 5)
        for label, name, email in (("author", an, ae), ("committer", cn, ce)):
            if (name, email) != (identity["name"], identity["email"]):
                problems.append(f"commit {sha[:12]}: {label} is '{name} <{email}>', not '{identity['name']} <{identity['email']}>'")
        problems += [f"commit {sha[:12]}: {problem}" for problem in trace_problems(profile, {"the message": message})]
    return problems


def sync(project: Path, check_only: bool) -> list[str]:
    """Put the canonical template and this guard into the project's .github; report what differs."""
    targets = {HERE / "pr_template.md": project / ".github" / TEMPLATE_NAME, Path(__file__).resolve(): project / ".github" / GUARD_NAME}
    differing = [str(target) for source, target in targets.items() if not target.is_file() or target.read_bytes() != source.read_bytes()]
    if not check_only:
        (project / ".github").mkdir(exist_ok=True)
        for source, target in targets.items():
            shutil.copyfile(source, target)
    return differing


def run_check(args: argparse.Namespace) -> int:
    profile = json.loads(Path(args.profile).read_text(encoding="utf-8"))
    template = Path(args.template).read_text(encoding="utf-8")
    event = json.loads(Path(args.event).read_text(encoding="utf-8"))["pull_request"]
    problems = check_pull_request(profile, template, event["title"], event.get("body") or "", event["head"]["ref"])
    if args.commits:
        problems += check_commits(profile, args.commits, Path(args.repo))
    for problem in problems:
        print(f"pr-guard: {problem}", file=sys.stderr)
    print("pr-guard: OK" if not problems else f"pr-guard: {len(problems)} problem(s)")
    return 1 if problems else 0


def run_sync(args: argparse.Namespace) -> int:
    differing = sync(Path(args.project).resolve(), args.check)
    for path in differing:
        print(f"pr-guard: {'differs' if args.check else 'wrote'}: {path}")
    return 1 if (differing and args.check) else 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="pr-guard", description="Check a pull request against the project template and identity, or sync the template and this guard into a project.")
    commands = parser.add_subparsers(dest="command", required=True)
    check = commands.add_parser("check", help="check the pull request of a GitHub event file")
    check.add_argument("--event", required=True, help="the GitHub event JSON (GITHUB_EVENT_PATH)")
    check.add_argument("--profile", default=".ai/project.json")
    check.add_argument("--template", default=f".github/{TEMPLATE_NAME}")
    check.add_argument("--commits", help="a revision range such as BASE..HEAD whose commits are checked too")
    check.add_argument("--repo", default=".")
    sync_parser = commands.add_parser("sync", help="copy the canonical template and guard into a project")
    sync_parser.add_argument("project")
    sync_parser.add_argument("--check", action="store_true", help="only report a difference (exit 1)")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    return run_check(args) if args.command == "check" else run_sync(args)


if __name__ == "__main__":
    raise SystemExit(main())
