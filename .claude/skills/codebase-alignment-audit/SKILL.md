---
name: codebase-alignment-audit
description: Audits an existing codebase for misalignment between its code, tests, comments, and documentation — and against an external spec (an assignment brief, API contract, or requirements doc) when one exists. Verifies claims empirically (build/vet/test, live end-to-end runs against a real running stack) instead of trusting what docs or comments say, cross-checks "required test case" style specs against actual test bodies rather than test names, and traces small asymmetries (one sibling field or code path handled differently from another with no stated reason) to find real bugs. Use this whenever the user asks to review a project, check if something "aligns", asks for a second pass or re-check after changes, wants to know if docs/comments still reflect current behavior.
---

# Codebase Alignment Audit

A codebase drifts from its own story faster than anyone expects: a comment describes what a function did six edits ago, a planning doc references a limitation that got fixed last week, a test asserts on a scenario the code no longer produces. None of this shows up in `git diff` review because each individual change looked correct in isolation — the drift is between files, not within one. This skill is a repeatable process for finding that drift, verifying claims rather than reading them, and fixing what's actually broken instead of what merely looks stale.

The core discipline underneath every step here: **don't trust a claim just because it's written down — including claims you yourself write down mid-session.** A comment saying "verified against Postgres" is a claim, not a fact, until something actually ran a query. A doc listing "required test cases" is a checklist, not evidence, until you've read the test bodies that supposedly satisfy it.

## Step 1 — Establish the source(s) of truth, and read them fully before touching code

Before opening a single implementation file, find and read whatever defines "correct" for this project: an assignment brief or spec (PDF, ticket, RFC), the project's own planning docs (README, API spec, ER diagram, architecture doc), and any running decision log. Read these completely, not skimmed — a requirement buried in paragraph three of a PDF is exactly the kind of thing that gets missed on a fast pass and then silently fails the review.

If there are multiple sources of truth, note where they might disagree — these are worth understanding before judging alignment.

## Step 2 — Verify structurally first, cheaply, before deep reading

Run whatever the language/ecosystem provides for free correctness signal before doing anything else: compile/build, static analysis (`go vet`, `tsc --noEmit`, linters), formatters, and the full test suite with coverage. This takes seconds to minutes and immediately tells you whether the codebase is even in a state where deeper review is worthwhile — no point auditing comments in a package that doesn't compile.

Do this even if the project's own docs claim "everything passes" — that claim is exactly the kind of thing this skill exists to verify, not assume.

## Step 3 — Cross-check "required scenario" specs against actual test bodies, not test names

Specs often enumerate required behavior as a checklist: "duplicate username → 409", "wrong hospital → 401", "replay of a rotated token → rejected". It's tempting to grep test function names against this list and call it covered — resist that. A test named `TestLogin_WrongHospital` proves nothing about what it actually asserts. Read the body: does it really set up the wrong-hospital condition, and does it check the specific status code / error / behavior the spec demands, or something adjacent that happens to pass?

Build an explicit mental (or written) checklist: one row per required scenario, mark it only after reading the assertion that would fail if the behavior regressed. Gaps found this way are usually real and worth closing — see Step 5.

## Step 4 — Verify live, end-to-end, whenever the stack can actually run

Unit tests with fakes/mocks prove the code does what its own author believed it should do. They cannot prove the code works against the real dependency it was written for (a real database's constraint behavior, a real reverse proxy's header handling, a real container network). If Docker, a dev server, or any other way to run the real stack is available, use it:

- Bring the whole system up for real (e.g., `docker compose up --build`).
- Exercise the actual API surface with real requests (`curl`, an HTTP client) — not just the happy path, but the specific edge cases the spec calls out: auth boundaries, cross-tenant isolation, replay/idempotency, graceful degradation when an optional dependency is missing.
- Check logs for the specific server-side behavior a spec might require but not surface in the response (e.g., "log this distinctly, but never fail the request").
- If something looks wrong on the first try, don't assume the code is broken — check your own test input first (encoding issues, quoting, stale tokens) before concluding there's a real bug. Re-verify with a cleaner reproduction before reporting a finding.

A codebase's own documentation may say a layer is "not yet execution-verified" — that's a direct invitation to go verify it, not a permanent disclaimer to leave alone.

## Step 5 — When you fill a gap yourself, protect blast radius

If Step 3 or Step 4 surfaces a real coverage gap and you're adding tests to close it, think about what your new test actually touches before running it — especially anything with shared or live state:

- A new integration test that truncates/resets tables must never point at the same database a running instance depends on. Use a separate database/schema/namespace, and verify it's actually separate before running anything destructive.
- Prefer tests that skip gracefully (not fail) when their real dependency (a live DB, an external service) isn't reachable, so the rest of the suite stays runnable in constrained environments.
- After adding tests that touch live infrastructure, re-verify the *original* system still works (re-run a smoke check against the app, not just the new tests) — a test suite that "passes" while quietly wiping the thing it was testing against is a failure, not a success.

If something breaks because of your own test design, treat it exactly like any other bug found during the audit: diagnose the real cause, fix it properly (isolate the resource), and record it — don't just patch around the symptom.

## Step 6 — Audit docs and comments against current code, not against each other

Documentation rot has a specific shape worth searching for directly:

- Grep for staleness markers: "not yet", "NOT execution-verified", "as of this writing", "no Docker available", "TODO", "currently", "still needs" — anything phrased as a temporary state is a candidate for having quietly become permanent or already resolved.
- For every concrete claim a doc makes (a count — "8 events", "4 endpoints", "13 fields" — a file listing, a "done" marker, a described behavior), check it against the current code, not against another doc that might be equally stale. Docs citing each other can create a closed loop that all agree with one another while none of them agree with reality.
- If a project has planning docs that are meant to stand alone (e.g., deliverables meant to be read independently, or a stated rule that one doc shouldn't lean on another for justification), check that rule holds — and re-check it after any further edits, since concurrent work can reintroduce exactly the reference that was removed.
- When code gains new, non-obvious behavior (a new file, a new validation rule, a new safety check), check whether the doc that's supposed to enumerate exactly this (a package/file layout doc, an API field table) was updated to match. A new file with no entry in a doc that claims to list every file is a real gap, not a nitpick.

## Step 7 — Hunt asymmetries: the single highest-yield bug-finding heuristic in this whole process

When two things are structurally similar — sibling fields in the same response, sibling code paths for sibling operations, sibling validation rules for sibling inputs — and one is handled one way while the other is handled differently, stop and ask why. Sometimes there's a real, documented reason. Often there isn't, and it's a bug: a normalization step (trim, cap, sanitize) added to one field's write path but never carried through to its response's read/echo path, a validation rule applied to one endpoint's field but not to another endpoint's structurally identical field, an error path handled with a specific sentinel for one similar-but-not-identical failure and generically for the other.

Concretely: trace each field/path from input to storage to output and check every hop actually got the same treatment, especially after any change that touched only one of the siblings (e.g., "added trimming to the service layer" — did every place that reads that value downstream get updated too, including HTTP handlers that might independently reference the raw request field instead of the service's return value?).

This is worth doing as a dedicated pass, separate from reading for obvious bugs — obvious bugs jump out; asymmetries require deliberately comparing two similar things side by side.

## Step 8 — Handle concurrent edits from a human working alongside you

If the person is actively editing the same files (visible via file-modification timestamps changing between your reads, or explicit "file changed on disk" notices), don't assume corruption or ignore it — always re-read before your next edit, and treat unexpected new content as real work to build on, not noise to work around. If it looks like a genuine improvement (as it usually will be), incorporate it; if a fix you're about to make conflicts with something they just changed, re-derive your fix against their current version rather than clobbering it. State plainly what you noticed changed and why you're building on it, so they're not surprised by content they didn't write showing up attributed to "current state."

## Step 9 — Report findings calibrated, not uniform

Not everything found in an audit is equally important — say so explicitly rather than listing everything with the same weight. A useful three-way split:

- **Real bugs, found and fixed** — behavior that was actually wrong, with a concrete failure scenario (not just "could theoretically be an issue"). Explain the failure scenario precisely enough that someone could reproduce it, and what was changed to fix it.
- **Doc/comment misalignment, fixed** — nothing behaviorally wrong, but something written down that no longer matches reality. Worth fixing for the next reader, but don't conflate with a real bug.
- **Checked and confirmed correct** — call out what you specifically verified and found fine, especially anything that looked suspicious at first glance (e.g., "looked like a race condition, reproduced twice, was actually a one-off startup-timing issue"). This tells the person what ground has actually been covered versus merely assumed fine.

## Reusable pattern: smoke-testing a typical CRUD-plus-auth service

For services shaped like "create an account, log in, get a token, act on scoped resources" (extremely common), this checklist tends to catch the same classes of bug regardless of language/framework:

1. Create a resource, confirm required fields round-trip correctly (including any normalization — trimming, casing) in the response, not just the DB.
2. Duplicate-creation conflict, scoped correctly (e.g., unique-per-tenant, not globally unique, if that's the design).
3. Log in successfully; confirm the issued token/session actually authorizes the next call.
4. Wrong credentials, wrong tenant/scope, and unknown identity should generally be indistinguishable to the caller if the spec cares about not leaking which part was wrong — check the error responses are actually identical, not just similarly worded.
5. Token/session refresh or rotation: old credential must stop working after rotation (replay rejection), not just "still work but also a new one exists."
6. Cross-tenant isolation: create two tenants, confirm a query that would match cross-tenant data returns nothing when scoped to the wrong tenant.
7. Graceful degradation: if the design has an optional dependency (e.g., an integration that might not be configured for every tenant), confirm the missing case fails soft (empty result, logged) rather than erroring the request.
8. Auth boundary: hit a protected endpoint with no token, a garbage token, and an expired token — each should fail the same way the spec expects, not with an unhandled 500.
