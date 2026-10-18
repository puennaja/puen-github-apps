# AI Reviewer GitHub App — proposed

**Status: design only; not registered or enabled.**

Independent reviewer identity (suggested `puen-reviewer`, name availability unverified). Read PR diff and relevant specs, comment with BLOCKER/MAJOR/MINOR and evidence, identify reviewed head SHA. Rerun review for each new head commit.

Potential permissions: Metadata read, Contents read, Pull requests read/write **only if needed to submit PR reviews or comments**; validate exact GitHub endpoint permissions first. Any broad write permission must be contained by broker allowlist. No repo code edits, merge, or human approvals.

No automatic reviewer is implemented. ChatGPT's existing GitHub connection does not automatically acquire this App identity. Provider/cost and unattended CI eligibility require separate verification.
