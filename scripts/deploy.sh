#!/bin/bash
# Pull the latest code and build frontend + backend.
set -e
cd "$(dirname "$0")/.."

git pull
(cd frontend && npm ci && npm run build)
(cd backend/server && go build -o ../../bin/wall .)

echo "Built. Restart run.sh to pick up the changes."
