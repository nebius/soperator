---
# Codex-triggered metadata
name: e2e-triage
description: Analyze root cause of soperator e2e test failures from a GitHub Actions run URL. Use when asked to triage, diagnose, or analyze a failed e2e test run.
# Claude compatibility metadata
argument-hint: '<github-actions-run-url>'
allowed-tools: Bash, Read, Glob, Grep, Agent, AskUserQuestion
---

# E2E Triage

Analyze a failed soperator e2e test run and produce a root cause summary.

Compatibility notes:
- This is a repo-local Codex skill bundle under `.agents/skills/`.
- The workflow text stays Claude-compatible where possible, but Claude may not auto-discover this location.
- Where the instructions mention `AskUserQuestion`, keep the Claude wording for compatibility. In Codex, ask the same question directly in a plain-text chat message.
- The only intentional command difference is script location: this repo-local version uses `.agents/skills/e2e-triage/scripts/e2e-split-logs.py` instead of `~/.claude/scripts/e2e-split-logs.py`.

## Phase 1: Download, split logs, and ask for Slack URL

**IMPORTANT: Before starting any downloads, immediately ask the user for the Slack thread URL.** This lets them find and paste it while downloads run.

Do not skip this step. If the user already provided a Slack thread URL, reuse it. Otherwise ask before any downloads, log reads, or other tool calls.

1. Use AskUserQuestion to ask: "Paste the Slack failure notification thread URL (or skip if none):"
   In Codex, ask the same question as a plain-text chat message.
2. In parallel, run `python3 .agents/skills/e2e-triage/scripts/e2e-split-logs.py <run-url>` to download and split logs
3. In parallel, fetch run metadata via `gh api repos/{repo}/actions/runs/{run_id} --jq '{date: .run_started_at, branch: .head_branch}'`

Save the Slack URL for use in Phase 4 (HTML output) and Phase 5 (Jira comment).

Read `steps.json` to get the overview of all steps and their conclusions.

## Phase 2: Investigate

1. Find the first step with `"conclusion": "failure"` — this is the root cause step. Later failures are usually consequences.
2. Read the last ~200 lines of the failed step's log file (errors are at the bottom).
3. Based on what you see, consult the **Debug Info Reference** below to decide which diagnostic steps to read next. Follow the evidence — there is no fixed decision tree.
4. If diagnostic step logs are not sufficient, download artifacts for deeper investigation:
   - `gh run download {run_id} -n cluster-info -D /tmp/e2e-triage-{run_id}/cluster-info` — full `kubectl cluster-info dump` (pod logs, events, resources) for namespaces: kruise-system, soperator-system, soperator, flux-system
   - `gh run download {run_id} -n jail -D /tmp/e2e-triage-{run_id}/jail` — Slurm config (`/etc/slurm/`) and soperator outputs (`/opt/soperator-outputs/`)

### Debug Info Reference

Diagnostic steps between Terraform Apply and Terraform Destroy:

| Step | What's inside | When to use |
|---|---|---|
| K8s Cluster Info and NodeGroups | Brief state for all node groups, full state for PROVISIONING ones (look at "events") | Terraform apply failed creating node groups. Events usually mean NER / quota / mk8s bug |
| K8s Cluster: Pods | Brief state of all pods | When no active check ran, to find the reason. Focus on soperator namespace: login-*, worker-*, controller-* |
| K8s Cluster: Events | Kubernetes cluster events | Rarely — e.g. when a pod cannot pull an image |
| K8s Cluster: Nodes | State of all k8s nodes | Rarely |
| K8s Cluster: Jobs | State of all k8s jobs | Rarely — e.g. when a k8s active check job failed |
| K8s Cluster: Helm Releases | Helm releases in flux-system namespace | When a flux HelmRelease install failed |
| K8s Cluster: Slurm Cluster CRs | SlurmCluster CRs | Rarely |
| K8s Cluster: Slurm Active Checks CRs | ActiveCheck CRs | When active checks helm release timed out / failed — shows which checks ran / failed |

### Domain knowledge

- NER = Not Enough Resources (Nebius cloud capacity issue, not a bug in soperator)
- Ignore `opentelemetry-collector-jail-logs-*` pod failures when no active check has run — this is normal, the collector starts after an active check creates jail folders
- Post-destroy cleanup steps are not failure causes
- When terraform destroy fails, always identify the specific resources that failed (resource type, name, ID) by searching for `Still destroying` and error lines in the destroy step log. Include them in the report.
- The HTML/Slack output must contain the same resource IDs as the terminal summary — they are needed for debugging.

## Phase 3: Search Jira for similar issues

Search for existing known e2e failure tickets:

```bash
acli jira workitem search --jql 'Labels = "soperator-e2e-fail" AND status NOT IN ("Done", "Won'\''t do")' --fields "key,summary,status" --limit 20
```

If `acli` is not installed, ask the user to install it. Official macOS install:

```bash
brew tap atlassian/homebrew-acli
brew install acli
```

Then ask the user to run:

```bash
acli auth login
```

for OAuth authorization.

If `acli` fails because Jira is unavailable or temporarily unreachable, retry the same command before concluding the search failed.

Compare your root cause against the ticket summaries. If a matching ticket exists, mention its key in the output. If no match is found, note that this may be a new issue.

## Phase 4: Output

Print a structured summary to the terminal, then write an HTML version for Slack to `/tmp/e2e-triage-<run_id>/slack-message.html` and print the full path.

Use `<b>` for labels, `<code>` for log snippets, `<br>` for line breaks, `<a href="...">` for Jira links. Keep it compact enough for a single Slack message. The user can open the HTML file in a browser and copy from there.

At the end, ask the user whether they want to open the HTML file in the browser. If they agree, run `open /tmp/e2e-triage-<run_id>/slack-message.html`.

## Phase 5: Report to Jira

**If a matching Jira ticket was found in Phase 3**, post a comment to it immediately (do NOT ask the user for confirmation):

Post a rich-formatted comment using Atlassian Document Format (ADF):

```bash
acli jira workitem comment create --key "SCHED-XXXX" --body '<ADF JSON>'
```

If `acli` is not installed, ask the user to install it. Official macOS install:

```bash
brew tap atlassian/homebrew-acli
brew install acli
```

Then ask the user to run:

```bash
acli auth login
```

for OAuth authorization.

If an `acli jira` command fails because Jira is unavailable or temporarily unreachable, retry the same command before giving up.

ADF is a JSON format. Build it with these node types:
- **Link**: `{"type":"text","text":"display text","marks":[{"type":"link","attrs":{"href":"URL"}}]}`
- **Code**: `{"type":"text","text":"code snippet","marks":[{"type":"code"}]}`
- **Bold**: `{"type":"text","text":"bold text","marks":[{"type":"strong"}]}`

Example ADF comment structure:

```json
{"version":1,"type":"doc","content":[{"type":"paragraph","content":[
  {"type":"text","text":"Reproduced in "},
  {"type":"text","text":"run #<run_id>","marks":[{"type":"link","attrs":{"href":"<run_url>"}}]},
  {"type":"text","text":" (<run_date>, <branch>). "},
  {"type":"text","text":"Slack thread","marks":[{"type":"link","attrs":{"href":"<slack_url>"}}]},
  {"type":"text","text":". <root cause summary with "},
  {"type":"text","text":"code snippets","marks":[{"type":"code"}]},
  {"type":"text","text":" where relevant."}
]}]}
```

**If no matching Jira ticket was found in Phase 3**, use AskUserQuestion to ask:
- Option 1: "Create a new Jira ticket with `soperator-e2e-fail` label"
- Option 2: "Investigate further"
- Option 3: "Done, no ticket needed"

In Codex, ask the same question as a plain-text chat message.

If creating a new ticket, use `acli jira workitem create` with the `soperator-e2e-fail` label, appropriate summary and description based on the root cause analysis, then post the triage comment to it as well.

If `acli` is not installed, ask the user to install it with:

```bash
brew tap atlassian/homebrew-acli
brew install acli
```

Then ask the user to run:

```bash
acli auth login
```

for OAuth authorization.
