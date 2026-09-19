#!/usr/bin/env bash
# Deploys api + mockserver to Cloud Run, backed by Neon Postgres.
# See docs/deploy-gcp.md for the full explanation and prerequisites.
#
# Run from anywhere: ./deploy-gcp.sh or deployments/deploy-gcp.sh
set -euo pipefail

cd "$(dirname "$0")"

# Config/secrets live in gcp.env, never in this script — gcp.env is
# gitignored as an exact filename (see ../.gitignore), so this script stays
# safe to commit even once you've filled in real values. Copy
# gcp.env.example to gcp.env and edit that instead.
if [[ -f gcp.env ]]; then
  set -a
  source gcp.env
  set +a
fi

: "${PROJECT_ID:?Set PROJECT_ID in deployments/gcp.env — copy gcp.env.example there first}"
: "${NEON_DATABASE_URL:?Set NEON_DATABASE_URL in deployments/gcp.env — copy gcp.env.example there first}"
REGION="${REGION:-us-central1}"  # one of Cloud Run's cheapest ("Tier 1") regions — see docs/deploy-gcp.md

REPO="hospital-middleware"
REGISTRY="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}"

echo "==> Configuring gcloud project and enabling required APIs"
gcloud config set project "$PROJECT_ID"
gcloud services enable run.googleapis.com artifactregistry.googleapis.com cloudbuild.googleapis.com

echo "==> Ensuring Artifact Registry repo exists"
gcloud artifacts repositories describe "$REPO" --location="$REGION" >/dev/null 2>&1 || \
  gcloud artifacts repositories create "$REPO" \
    --repository-format=docker --location="$REGION" \
    --description="Hospital Middleware images"

gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet

echo "==> Running migrations against Neon"
# MSYS_NO_PATHCONV=1: Git Bash (MSYS) auto-converts any bare argument that
# looks like an absolute POSIX path into a Windows path before handing it
# to a native .exe like docker.exe — without this, both the volume mount's
# ":/migrations" target and "-path=/migrations" get silently mangled
# before docker ever sees them, and the container fails with "open .: no
# such file or directory" (migrate falling back to "." with nothing
# actually mounted there). Confirmed by reproducing it directly. Harmless
# on Linux/macOS — MSYS_NO_PATHCONV only means anything to Git Bash.
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd)/../migrations:/migrations" migrate/migrate:v4.17.1 \
  -path=/migrations -database "$NEON_DATABASE_URL" up

echo "==> Building + pushing mockserver"
docker build --target mockserver -t "${REGISTRY}/mockserver:latest" -f Dockerfile ..
docker push "${REGISTRY}/mockserver:latest"

echo "==> Deploying mockserver (api needs its URL next)"
gcloud run deploy mockserver \
  --image "${REGISTRY}/mockserver:latest" \
  --region "$REGION" --port 9000 --allow-unauthenticated --min-instances=0 --quiet

MOCKSERVER_URL=$(gcloud run services describe mockserver --region "$REGION" --format='value(status.url)')
echo "    mockserver live at: $MOCKSERVER_URL"

echo "==> Building + pushing api"
docker build --target api -t "${REGISTRY}/api:latest" -f Dockerfile ..
docker push "${REGISTRY}/api:latest"

JWT_SECRET=$(openssl rand -base64 32)

echo "==> Deploying api"
gcloud run deploy api \
  --image "${REGISTRY}/api:latest" \
  --region "$REGION" --port 8080 --allow-unauthenticated --min-instances=0 --quiet \
  --set-env-vars "DATABASE_URL=${NEON_DATABASE_URL},JWT_SECRET=${JWT_SECRET},BASE_URL_HOSPITAL_A=${MOCKSERVER_URL}"

API_URL=$(gcloud run services describe api --region "$REGION" --format='value(status.url)')

echo ""
echo "==> Done"
echo "    api:        $API_URL"
echo "    mockserver: $MOCKSERVER_URL"
echo "    Swagger UI: ${API_URL}/docs"
echo ""
echo "JWT_SECRET was generated fresh and is NOT saved anywhere else — if you lose it,"
echo "redeploying with a new one just invalidates existing tokens (staff re-login)."
echo "    JWT_SECRET=${JWT_SECRET}"
