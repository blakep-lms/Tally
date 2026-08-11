#!/usr/bin/env python3
"""Validate and package generated OpenWiki output for the code-map branch."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
from datetime import datetime, timezone
from pathlib import Path

DENY_NAMES = {"CLAUDE.md", "AGENTS.md", ".env", "graph.db", "checkpoint.sqlite", "openwiki-update.yml"}
SECRET_RE = re.compile(
    r"(sk-[A-Za-z0-9_-]{20,}|gh[oprsu]_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|"
    r"-----BEGIN [A-Z ]*PRIVATE KEY-----)"
)
FIELDS = {
    "authority": "derived-noncanonical",
    "canonical": "false",
    "xtrace_ingest": "deny",
    "generated_by": "openwiki@0.3.1",
}
LINK_RE = re.compile(r"\[([^\]]+)\]\((?!https?://|mailto:|#)([^)#]+)(#[^)]*)?\)")
BROKEN_COMMENT_RE = re.compile(r"(?m)^<!-- openwiki: broken internal link .*?-->\n?")


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def stamp_markdown(path: Path, wiki_root: Path, commit: str) -> None:
    text = path.read_text(errors="strict")
    text = text.replace(
        "canonical source-grounded code map",
        "derived, source-grounded code map",
    ).replace(
        "Fully Implemented",
        "Present in tracked source; runtime unverified",
    )

    def portable_link(match: re.Match[str]) -> str:
        label, target, anchor = match.group(1), match.group(2), match.group(3) or ""
        direct = (path.parent / target).resolve()
        destination = direct
        if target.startswith("/openwiki/"):
            destination = (wiki_root / target.removeprefix("/openwiki/")).resolve()
        elif not direct.exists():
            stripped = target
            while stripped.startswith("../"):
                stripped = stripped[3:]
            candidate = (wiki_root / stripped).resolve()
            if candidate.exists() or candidate.is_dir():
                destination = candidate
        if destination.is_dir():
            destination = destination / "index.md"
        if not destination.exists():
            return match.group(0)
        relative = os.path.relpath(destination, path.parent.resolve())
        return f"[{label}]({Path(relative).as_posix()}{anchor})"

    text = LINK_RE.sub(portable_link, text)
    fields = {**FIELDS, "source_commit": commit}
    if text.startswith("---\n"):
        end = text.find("\n---\n", 4)
        if end == -1:
            raise ValueError(f"unclosed frontmatter: {path}")
        frontmatter = text[4:end]
        for key, value in fields.items():
            if not re.search(rf"(?m)^{re.escape(key)}\s*:", frontmatter):
                frontmatter += f"\n{key}: {value}"
        text = "---\n" + frontmatter + text[end:]
    else:
        block = "\n".join(f"{key}: {value}" for key, value in fields.items())
        text = f"---\ntype: derived-code-map\n{block}\n---\n\n{text}"
    path.write_text(text)


def validate_links(wiki_root: Path) -> list[str]:
    broken: list[str] = []
    for path in wiki_root.rglob("*.md"):
        text = path.read_text(errors="strict")
        for match in LINK_RE.finditer(text):
            target = match.group(2)
            destination = (path.parent / target).resolve()
            if destination.is_dir():
                destination = destination / "index.md"
            try:
                destination.relative_to(wiki_root.resolve())
            except ValueError:
                broken.append(f"{path.relative_to(wiki_root)} -> {target} (outside wiki)")
                continue
            if not destination.exists():
                broken.append(f"{path.relative_to(wiki_root)} -> {target}")
    return broken


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", default="openwiki")
    parser.add_argument("--output", required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--ref", required=True)
    parser.add_argument("--model", required=True)
    parser.add_argument("--project", required=True)
    parser.add_argument("--canonical-vault-owner", required=True)
    parser.add_argument("--title", required=True)
    args = parser.parse_args()

    source = Path(args.source).resolve()
    output = Path(args.output).resolve()
    if not source.is_dir():
        raise SystemExit("generated openwiki directory is missing")
    markdown = sorted(source.rglob("*.md"))
    if len(markdown) < 3 or not (source / "index.md").exists():
        raise SystemExit("generated wiki is incomplete")

    violations: list[str] = []
    for path in source.rglob("*"):
        if not path.is_file():
            continue
        relative = path.relative_to(source)
        if path.name in DENY_NAMES or ".github" in path.parts or ".claude" in path.parts:
            violations.append(str(relative))
        if SECRET_RE.search(path.read_text(errors="ignore")):
            violations.append(f"secret-pattern:{relative}")
    if violations:
        raise SystemExit("generated output rejected: " + ", ".join(violations))

    if output.exists():
        shutil.rmtree(output)
    wiki = output / "wiki"
    shutil.copytree(source, wiki)
    for path in wiki.rglob("*.md"):
        stamp_markdown(path, wiki, args.commit)
    broken = validate_links(wiki)
    if broken:
        raise SystemExit("generated output has broken internal links: " + "; ".join(broken))
    for path in wiki.rglob("*.md"):
        text = path.read_text(errors="strict")
        path.write_text(BROKEN_COMMENT_RE.sub("", text))

    manifest = [
        {"path": path.relative_to(output).as_posix(), "sha256": digest(path)}
        for path in sorted(output.rglob("*"))
        if path.is_file()
    ]
    mermaid_blocks = sum(path.read_text(errors="ignore").count("```mermaid") for path in wiki.rglob("*.md"))
    provenance = {
        "schema": "lms-derived-code-map-v1",
        "type": "derived-code-map",
        "authority": "derived-noncanonical",
        "canonical": False,
        "xtrace_ingest": "deny",
        "project": args.project,
        "canonical_vault_owner": args.canonical_vault_owner,
        "repository": args.repository,
        "source_ref": args.ref,
        "source_commit": args.commit,
        "openwiki_version": "0.3.1",
        "provider": "gemini",
        "model": args.model,
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "wiki_files": len(markdown),
        "mermaid_blocks": mermaid_blocks,
        "manifest": manifest,
    }
    (output / "provenance.json").write_text(json.dumps(provenance, indent=2) + "\n")
    (output / "README.md").write_text(
        f"# DERIVED CODE MAP — {args.title}\n\n"
        f"Generated from `{args.repository}` at `{args.commit}`.\n\n"
        "This branch is replaceable, source-pinned implementation evidence. "
        "It is not source code, LMS-Vault authority, requirements, handoff, or task state.\n\n"
        "Start with [`wiki/index.md`](wiki/index.md).\n"
    )
    print(json.dumps({"status": "ok", "wiki_files": len(markdown), "mermaid_blocks": mermaid_blocks}, indent=2))


if __name__ == "__main__":
    main()
