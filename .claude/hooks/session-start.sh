#!/bin/bash
# Richtet die Umgebung für Claude Code on the web ein: Abhängigkeiten installieren
# und Build-Caches vorwärmen, damit Tests und Linter sofort laufen.
# Mehrfach ausführbar (idempotent), ohne Rückfragen.
set -euo pipefail

# Nur in der Remote-Umgebung ausführen; lokale Rechner bleiben unberührt.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

ROOT="${CLAUDE_PROJECT_DIR:-$(cd "$(dirname "$0")/../.." && pwd)}"
cd "$ROOT"

# Node: package.json hat derzeit keine Abhängigkeiten; der Aufruf legt node_modules
# an und trägt spätere Pakete automatisch nach.
if [ -f package.json ]; then
  echo "==> npm install"
  npm install --no-audit --no-fund
fi

# Python: literary_analysis.py benötigt das Anthropic-SDK. Es gibt keine
# requirements.txt, daher wird das Paket direkt installiert (vorhandene
# Installation wird übersprungen).
echo "==> pip install"
PIP_OPTS="--quiet --disable-pip-version-check --root-user-action=ignore"
if [ -f requirements.txt ]; then
  pip install $PIP_OPTS -r requirements.txt
else
  pip install $PIP_OPTS anthropic
fi

# Go: keine externen Module. Build- und Vet-Cache vorwärmen, damit
# `go test ./...` und `go vet ./...` später ohne Wartezeit laufen.
# `go test -run '^$'` übersetzt alles einschließlich der Testdateien, führt aber
# nichts aus und legt – anders als `go build` – keine Binärdatei im Arbeitsbaum ab.
if [ -f zeiterfassung/go.mod ]; then
  echo "==> go test/vet (Cache vorwärmen)"
  (cd zeiterfassung && go test -run '^$' ./... >/dev/null && go vet ./...)
fi

echo "==> Setup abgeschlossen"
