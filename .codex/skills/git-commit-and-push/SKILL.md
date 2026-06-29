---
name: git-commit-and-push
description: Use when the user asks to commit changes, prepare a git commit message, stage files, commit, or push changes while checking for generated, local-only, or sensitive files first.
---

# Git Commit and Push Workflow

## Overview

Use this skill to prepare, confirm, commit, and push git changes while avoiding accidental commits of generated, local-only, or sensitive files. Review only git metadata, file names, directory names, and paths unless the user explicitly asks for content review.

## Workflow

1. Run `git status --short` and review the changed file list.
2. If more context is needed, use metadata-only commands such as `git status`, `git diff --name-only`, `git diff --stat`, `git ls-files --others --exclude-standard`, or `git branch --show-current`.
3. Check for files that appear to be generated, temporary, local-only, build artifacts, IDE files, logs, caches, secrets, credentials, or environment-specific files.
4. Do not open, read, diff, or review file contents unless the user explicitly requests it. Use only git metadata, file names, directory names, and file paths to understand the scope of changes.
5. If any files should likely be added to `.gitignore`, ask for confirmation before modifying `.gitignore` or proceeding with the commit.
6. Create a concise commit title that clearly summarizes the change:
   - Use a short sentence.
   - Make it easy to understand when scanning git history.
   - Avoid vague messages such as "updates", "fixes", or "changes".
   - Focus on the outcome of the work.
7. Create a commit description that:
   - Summarizes the main changes included in the commit.
   - Uses clear professional language.
   - Is concise and practical.
   - Does not sound academic, overly formal, or AI-generated.
   - Focuses on what changed.
8. Show the proposed commit title and description, list the files that will be staged by path, and ask for confirmation.
9. After confirmation, execute the commit and push with non-interactive git commands:

```bash
git add .
git commit -m "<commit title>" -m "<commit description>"
git push
```

Use the generated commit title as the commit subject and the generated description as the commit body.

## Safety Checks

- Treat paths containing `.env`, `secret`, `credential`, `token`, `key`, `pem`, `p12`, `log`, `cache`, `tmp`, `temp`, `dist`, `build`, `coverage`, `.DS_Store`, `.idea`, or `.vscode` as suspicious until confirmed.
- Flag package manager lockfiles as intentional dependency metadata, not suspicious by default.
- If the repository has no commits, no current branch, or no push remote, report the situation and ask how to proceed.
- If `git push` fails because no upstream branch is configured, ask before setting upstream or changing remotes.
- If the user asks only for a commit message, produce the title and description without staging, committing, or pushing.

## Confirmation

Never commit or push without user confirmation of the exact title, description, and intended file scope immediately before running git write commands.
