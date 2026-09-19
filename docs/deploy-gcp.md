# Deploying to GCP (Cloud Run + Neon)

This deploys `api` and `mockserver` as two Cloud Run services, backed by [Neon](https://neon.tech) (a free, serverless Postgres) instead of Cloud SQL — both free tiers are perpetual, not trials. `nginx` isn't deployed here: Cloud Run already terminates TLS and handles ingress, so its one job in `docker-compose.yml` doesn't apply to this setup. `docker-compose.yml` itself is untouched and still works for local dev exactly as before — this is an additional deployment path, not a replacement.

See `docs/DECISION_LOG.md`'s Deployment section for the full reasoning behind Cloud Run + Neon over Cloud SQL.

## Prerequisites (one-time, manual — can't be scripted on your behalf)

1. **A GCP project with billing enabled.** Cloud Run's free tier is real, but GCP still requires a billing account attached to the project even if you stay within the free tier.
2. **[`gcloud` CLI](https://cloud.google.com/sdk/docs/install) installed and authenticated**: `gcloud init` then `gcloud auth login`.
3. **A [Neon](https://neon.tech) account and project.** Create one, then grab its connection string from the Neon dashboard — it looks like:
   ```
   postgresql://<user>:<password>@<endpoint>.neon.tech/<dbname>?sslmode=require
   ```
4. **Docker**, already required for local dev — reused here to build and push images.

## What the script does

`deployments/deploy-gcp.sh` automates everything *after* the prerequisites above:

1. Enables the required GCP APIs (Cloud Run, Artifact Registry, Cloud Build) and creates an Artifact Registry repo, idempotently.
2. Runs `migrations/000001` through `000003` against your Neon database (the same `migrate/migrate` image `docker-compose.yml` already uses — nothing migration-specific to this deploy path).
3. Builds and pushes the `mockserver` image, deploys it to Cloud Run first (`api` needs its URL).
4. Builds and pushes the `api` image, deploys it to Cloud Run with `DATABASE_URL` (your Neon string), a freshly generated `JWT_SECRET` (never reuses the local dev placeholder), and `BASE_URL_HOSPITAL_A` set to the just-deployed mockserver's URL.
5. Prints the live `api` URL at the end.

## Before running it

Config and secrets live in a separate, **gitignored** file — `deploy-gcp.sh` itself never contains real values, so it stays safe to commit even after you've set everything up.

```bash
cd deployments
cp gcp.env.example gcp.env
```

Then edit `gcp.env` (not `gcp.env.example`, not the script) and fill in:

- `PROJECT_ID` — your GCP project id.
- `REGION` — `us-central1` is left as the default; it's one of Cloud Run's cheapest ("Tier 1") regions and is included in the free tier's request/compute allocation. Change it if latency to a specific place matters more to you than that.
- `NEON_DATABASE_URL` — the connection string from step 3 above.

Then:

```bash
chmod +x deploy-gcp.sh
./deploy-gcp.sh
```

## After it's live

- Both services deploy with `--allow-unauthenticated` — this makes them reachable over the public internet with no Cloud Run/IAM-level auth, which is what you want for a demo link, but it means `/staff/create` and `/staff/login` (which the app itself leaves unauthenticated — see `docs/api-spec.md`'s Assumption 1) are reachable by anyone who has the URL. `/patient/search` still requires a valid Bearer token from the app's own JWT auth regardless — Cloud Run's `--allow-unauthenticated` only controls the platform-level gate, not the application's own auth.
- Both services scale to zero when idle (Cloud Run's default) — the first request after a quiet period will have a cold start (a few seconds, mostly Go binary startup, no heavy runtime to warm up).
- Re-running the script after a code change rebuilds and redeploys both images — safe to run again; `gcloud run deploy` replaces the existing revision.
- The generated `JWT_SECRET` is printed once by the script and not saved anywhere — if you lose it and redeploy with a new one, every previously issued access/refresh token stops validating (staff just need to log in again; nothing destructive).

## Rough cost expectation

Within Cloud Run's free tier (2M requests, 180,000 vCPU-seconds, 1 GiB egress/month) and Neon's free tier (100 CU-hours, 0.5 GB storage/month), a demo/take-home-review level of traffic should cost **$0**. The one thing to watch is `docker push` traffic to Artifact Registry and Cloud Build minutes on the *first* deploy and any rebuild — Cloud Build also has its own free tier (120 build-minutes/day) that a project this size won't come close to using.
