# Triage label vocabulary

These are the five canonical triage states used by the `triage` skill.
For this repo they appear as the `status` field in `.scratch/*/issue.md`.

| Role | Value | Meaning |
|---|---|---|
| Needs evaluation | `needs-triage` | Newly filed; a maintainer must assess it |
| Waiting on reporter | `needs-info` | Blocked — need more detail from whoever filed it |
| Ready for agent | `ready-for-agent` | Fully specified; an AFK agent can action it with no extra context |
| Ready for human | `ready-for-human` | Needs human judgment or implementation |
| Won't fix | `wontfix` | Deliberately not being actioned |

## Usage

When the `triage` skill moves an issue between states it sets the `status`
frontmatter field to one of the values in the table above. No external label
API is involved.
