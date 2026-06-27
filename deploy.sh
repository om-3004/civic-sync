#!/usr/bin/env bash
# deploy.sh — Submit a Cloud Build job to build and deploy CivicSync to Cloud Run.
#
# Usage:
#   ./deploy.sh [PROJECT_ID] [REGION]
#
# Arguments (both optional; env vars are checked as fallback):
#   PROJECT_ID   — GCP project ID. Falls back to $PROJECT_ID env var or
#                  the active gcloud project if neither is set.
#   REGION       — GCP region for Artifact Registry + Cloud Run.
#                  Falls back to $REGION env var, then defaults to us-central1.
#
# Examples:
#   ./deploy.sh my-gcp-project
#   ./deploy.sh my-gcp-project europe-west1
#   PROJECT_ID=my-gcp-project ./deploy.sh
#
# Prerequisites:
#   - gcloud CLI authenticated and pointing at the right account
#   - Artifact Registry repository "civic-sync" created in $_REGION:
#       gcloud artifacts repositories create civic-sync \
#         --repository-format=docker --location=us-central1
#   - Secret Manager secrets created:
#       echo -n "YOUR_KEY" | gcloud secrets create gemini-api-key --data-file=-
#       echo -n "YOUR_PIN" | gcloud secrets create master-pin --data-file=-
#   - Cloud Build service account has roles:
#       roles/run.admin, roles/iam.serviceAccountUser,
#       roles/artifactregistry.writer, roles/secretmanager.secretAccessor

set -euo pipefail

# ── Resolve PROJECT_ID ────────────────────────────────────────────────────────
PROJECT_ID="${1:-${PROJECT_ID:-}}"
if [[ -z "$PROJECT_ID" ]]; then
  PROJECT_ID="$(gcloud config get-value project 2>/dev/null || true)"
fi
if [[ -z "$PROJECT_ID" ]]; then
  echo "Error: PROJECT_ID is not set." >&2
  echo "Provide it as the first argument, set the PROJECT_ID env var," >&2
  echo "or configure a default project with: gcloud config set project PROJECT_ID" >&2
  exit 1
fi

# ── Resolve REGION ────────────────────────────────────────────────────────────
REGION="${2:-${REGION:-us-central1}}"

# ── Print deploy summary ─────────────────────────────────────────────────────
echo "========================================"
echo "  CivicSync — Cloud Build Deploy"
echo "========================================"
echo "  Project : $PROJECT_ID"
echo "  Region  : $REGION"
echo "  Config  : cloudbuild.yaml"
echo "========================================"
echo ""

# ── Submit the build ──────────────────────────────────────────────────────────
gcloud builds submit \
  --project="$PROJECT_ID" \
  --config=cloudbuild.yaml \
  --substitutions="_REGION=${REGION}" \
  .

echo ""
echo "Deploy submitted. Track progress at:"
echo "  https://console.cloud.google.com/cloud-build/builds?project=${PROJECT_ID}"
