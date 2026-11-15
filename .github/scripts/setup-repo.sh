#!/bin/bash
# Applies the repository settings that can't live in files. Run once after creating the repository on GitHub,
# as a repository admin, with the GitHub CLI logged in:   .github/scripts/setup-repo.sh OWNER/REPO
#   - merging only by rebase (no merge commits, no squash), auto-merge allowed, branches deleted after merge
#   - rules for main: changes only through pull requests, required checks green and up to date, linear history,
#     no force pushes, no deletion
#   - Dependabot alerts and Dependabot security updates on, private vulnerability reporting on
#   - GitHub Actions may open pull requests (release-please)
set -euo pipefail
repo=${1:?usage: setup-repo.sh OWNER/REPO}

gh api -X PATCH "repos/$repo" \
  -F allow_merge_commit=false -F allow_squash_merge=false -F allow_rebase_merge=true \
  -F allow_auto_merge=true -F delete_branch_on_merge=true >/dev/null
echo "merge settings: rebase only, auto-merge allowed"

gh api -X PUT "repos/$repo/vulnerability-alerts" >/dev/null
gh api -X PUT "repos/$repo/automated-security-fixes" >/dev/null
gh api -X PUT "repos/$repo/private-vulnerability-reporting" >/dev/null
echo "Dependabot alerts + security updates, private vulnerability reporting: on"

gh api -X PUT "repos/$repo/actions/permissions/workflow" \
  -f default_workflow_permissions=read -F can_approve_pull_request_reviews=true >/dev/null
echo "Actions: read-only token by default, may open pull requests"

existing=$(gh api "repos/$repo/rulesets" --jq '.[] | select(.name == "main") | .id')
method=POST path="repos/$repo/rulesets"
[ -n "$existing" ] && method=PUT path="repos/$repo/rulesets/$existing"
gh api -X "$method" "$path" --input - >/dev/null <<'JSON'
{
  "name": "main",
  "target": "branch",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["~DEFAULT_BRANCH"], "exclude": [] } },
  "bypass_actors": [ { "actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "pull_request" } ],
  "rules": [
    { "type": "deletion" },
    { "type": "non_fast_forward" },
    { "type": "required_linear_history" },
    { "type": "pull_request", "parameters": {
        "required_approving_review_count": 0,
        "dismiss_stale_reviews_on_push": true,
        "require_code_owner_review": false,
        "require_last_push_approval": false,
        "required_review_thread_resolution": true,
        "allowed_merge_methods": ["rebase"] } },
    { "type": "required_status_checks", "parameters": {
        "strict_required_status_checks_policy": true,
        "required_status_checks": [
          { "context": "test (ubuntu-latest)" },
          { "context": "test (windows-latest)" },
          { "context": "test (macos-latest)" },
          { "context": "quality" },
          { "context": "commits" }
        ] } }
  ]
}
JSON
echo "rules for main: pull requests only, required checks, linear history, no force push/deletion"
