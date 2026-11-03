# Tracking upstream rebases

This repository is a downstream fork of
[`kubernetes-sigs/dra-driver-cpu`](https://github.com/kubernetes-sigs/dra-driver-cpu).
Keep downstream changes on top of the matching upstream branch. Use `main` for
`main`, and `release-vX.Y` for the same upstream release branch.

## Prepare

Configure the upstream remote once, then fetch both repositories before starting
a rebase:

```bash
git remote add upstream https://github.com/kubernetes-sigs/dra-driver-cpu.git
git fetch upstream
git fetch origin
```

If `upstream` already exists, just fetch it. Check which commits arrived
upstream and which downstream commits need to be carried forward:

```bash
git log --oneline origin/main..upstream/main
git log --oneline upstream/main..origin/main
```

Replace `main` with the matching release branch when rebasing a release.
Downstream-only commits should use the `UPSTREAM: <carry>:` subject prefix so
they are easy to identify and review.

see [`openshift/kubernetes rebase`](https://github.com/everettraven/openshift-kubernetes/blob/master/REBASE.openshift.md#picking-commits-from-the-previous-rebase-branch-to-the-new-branch) for reference.

## Rebase and review

Create a working branch from the downstream branch and replay it onto the latest
matching upstream branch:

```bash
git switch -c rebase-main origin/main
git rebase upstream/main
```

Resolve conflicts in each carried commit, keeping the downstream behavior that
is still needed. If upstream has incorporated a carried change, drop that
duplicate instead of carrying it again. Preserve the `UPSTREAM: <carry>:` prefix
on downstream commits.

Review the resulting commits and diff against upstream:

```bash
git log --oneline upstream/main..HEAD
git diff --stat upstream/main...HEAD
git diff upstream/main...HEAD
```

Run the relevant build and tests for the changes. Follow
[`docs/dev/testing.md`](docs/dev/testing.md) for repository testing guidance.

## Track the rebase in its PR

Open a PR against the matching downstream branch. Include:

- The upstream branch and commit being used as the new base.
- The downstream carries kept, changed, or dropped, with a short reason for
  changes and drops.
- Conflict resolutions that need reviewer attention.
- The build and test commands run and their results.

After the PR merges, use that updated downstream branch as the starting point
for the next rebase. This PR record and the `UPSTREAM: <carry>:` commit subjects
provide the history of what was brought forward and why.
