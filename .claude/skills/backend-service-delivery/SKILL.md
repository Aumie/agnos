---
name: backend-service-delivery
description: Working discipline for building and extending a backend service (API + database, often with Docker/docker-compose) so it's actually delivery-ready, not just compiling. Covers verifying every change immediately (build/vet/fmt/test), exercising a real running stack end-to-end instead of trusting unit tests alone, keeping a running decision log with the "why" behind non-obvious choices, keeping planning docs in sync with code in the same turn, reproducing reported anomalies before explaining them, presenting architecture trade-offs instead of unilaterally deciding, adding a regression test for every bug fixed, and never claiming verification that didn't actually happen. Use this whenever the user asks to build, extend, fix, or review a backend service/API — especially one with a database, Docker, or written planning docs (API spec, ER diagram, README) — even when they only ask for "the code" or say something "looks done," since verification and doc-sync are exactly the steps that get skipped under time pressure.
---

# Backend Service Delivery

Writing code that compiles and passes unit tests against fakes is the easy 80%. The last 20% — does it actually run against the real database, do the docs still describe what the code does, is there a paper trail for why a non-obvious choice was made — is what separates something merely finished from something actually deliverable. This skill is the discipline for not skipping that last 20%, especially under the time pressure that "just add this feature" request naturally creates.

The throughline underneath every step here: **a claim you make (including "this works," "this is tested," "this is documented") is only true once you've verified it, not once you've written code that plausibly does it.**

## 1. Verify every change immediately, before saying it's done

After any code edit — however small — run whatever the language/ecosystem gives you for free: build, static analysis (`go vet`, `tsc --noEmit`, a linter), a formatter check, and the test suite. This is seconds of work and it's the cheapest possible check against reporting something as done that doesn't even compile. Do this after *every* meaningful change, not batched at the end of a session — catching a break immediately, while the change is still fresh, is far cheaper than discovering it three features later.

## 2. When real infrastructure is reachable, actually exercise it — don't stop at unit tests

Unit tests against hand-written fakes prove the code does what its own author believed it should do. They cannot prove it works against the real dependency it was written for — a real database's constraint and transaction behavior, a real reverse proxy's header handling, a real container network, a real third-party API's actual response shape. If Docker, a dev server, or any other way to run the real stack exists, use it:

- Bring the whole system up for real (`docker compose up --build`, not just `go build`).
- Hit the actual API surface with real requests — not just the happy path, but auth boundaries, cross-tenant isolation, pagination edges, and whatever graceful-degradation behavior the design calls for.
- Check container logs for server-side-only behavior (things that should be logged but never surfaced as a request error).
- Query the database directly when a response alone can't confirm what actually got persisted.
- If a fix looks unverifiable in the current environment (no Docker, no network), say so plainly rather than reporting success anyway — see §9.

A codebase's own comment saying a layer is "not yet execution-verified" is an invitation to go verify it now that the means exist, not a permanent disclaimer to leave alone forever.

## 3. Keep a running decision log — the "why," organized by topic, updated the same turn a decision is made

Every non-obvious choice (a field added beyond what the spec literally asked for, a security tradeoff, a naming decision reversed after discussion, a library rejected and why) belongs in one running log — organized by topic (architecture, data model, auth, testing, tooling) so related decisions stay together, not chronologically, since a reader six weeks later cares about *what was decided about auth* far more than *what order things happened in*. Update it in the same turn the decision lands, not as a batch cleanup pass later — decisions made mid-conversation are the ones most likely to be forgotten if their reasoning isn't captured immediately.

Write entries as **the decision, then why** — the "why" is the entire value of the log; a list of what changed is already visible in git history.

## 4. Keep planning docs in sync with code, in the same turn

Whenever a code change would make a planning doc (API spec, ER diagram, project-structure doc, README) inaccurate, fix the doc in the same turn — not as a separate cleanup pass, and not only when explicitly asked. Treat what a doc currently claims as a claim to verify against the code, not a fact — docs citing each other can create a closed loop that all agree with one another while none of them agree with reality. A new file, a new validation rule, a new event type, or a changed error code should be reflected everywhere it's already documented before moving on.

## 5. When investigating a reported anomaly, reproduce it before explaining it

If the user reports something unexpected, don't reach for the most plausible-sounding explanation first — reproduce it (curl it, query the database, take a screenshot, read the actual logs) and let what you find drive the explanation. Two failure modes to avoid equally: assuming it's a bug in the code without checking your own repro steps first (stale token, wrong id, a typo in the request), and assuming it's "probably fine" without actually checking. When the anomaly turns out to be a real bug, keep digging past the first plausible cause to the actual root cause — a symptom fixed without finding why it happened tends to resurface elsewhere. (A test suite that silently truncates the same database a running dev stack depends on, discovered by actually querying the table rather than trusting "tests pass," is the kind of root cause this step is built to catch.)

## 6. For architecture and design questions, lay out trade-offs and let the person decide

When there's a genuine choice (explicit registration vs. auto-discovery, sync vs. async event delivery, mediator dispatch vs. direct calls, one database vs. two), present the concrete trade-off — what each option costs and buys, in the specific context of this codebase, not generic pros/cons — rather than unilaterally picking and only explaining afterward. If the person pushes back on a recommendation, engage honestly with the specific reasoning behind the pushback instead of either capitulating immediately or just repeating the original recommendation more firmly; if they want the rejected option actually built, build it for real rather than a half-hearted version, and if it's later reverted, capture *why* in the decision log (§3) — the arc is often more instructive than either endpoint alone.

## 7. Every bug fixed gets a regression test — and every non-obvious behavior gets a test proving it, positive and negative

A bug fixed without a test that would have caught it is a bug that can silently come back. When you fix something (a validation gap, a missing normalization step, a doc/behavior mismatch that turned out to be a real code issue), add the test alongside the fix, in the same change — not as a follow-up. The same applies to intentionally non-obvious behavior (a security-motivated design choice, a deliberately collapsed error response, a graceful-degradation path): if it's surprising enough to need a code comment explaining why, it's surprising enough to deserve a test proving it actually behaves that way.

## 8. Prefer the ecosystem's own idioms over patterns imported from a more familiar language — but earn that by explaining why

When the person's instinct reaches for a pattern from a language/framework they know better (a DI container, a mediator/command-bus, a Clean-Architecture-style layer split) in a codebase written in something with different conventions, it's worth pushing back — but with the concrete reason specific to *this* language's constraints and ecosystem culture, not just "that's not how we do it here." If they ask a direct, reasoned question about whether the recommendation was right, answer it honestly rather than defending the original call for its own sake; a decision reversed after a good challenge, with the reasoning captured in the decision log, is a better outcome than a decision defended past the point it still made sense.

## 9. Never fabricate verification

Only claim something is tested, working, or verified after actually checking it. If the environment can't support real verification right now (no Docker available, no network access, a dependency that can't be reached), say exactly that, explicitly, rather than describing an unverified change the way a verified one would be described. This applies just as much to your own decision-log/doc entries as to what you tell the person directly — a comment saying "verified against Postgres" is itself a claim that needs to have actually happened.

## Reusable rhythm for a typical change

1. Make the change.
2. Verify locally (§1) — build/vet/fmt/test.
3. Verify live if the real stack is reachable (§2).
4. Sync any doc the change makes inaccurate (§4).
5. Log the decision if it was non-obvious (§3).
6. Add a regression test if this was a bug fix or new non-obvious behavior (§7).
7. Report what changed and what's still open — calibrated, not uniform: a real bug fixed and verified reads differently from a doc-only correction, which reads differently from something you couldn't verify at all (§9).
