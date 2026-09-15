#!/bin/bash
# Richtet die Umgebung für Claude Code on the web ein: Abhängigkeiten installieren
# und Caches vorwärmen, damit Tests und Linter sofort laufen.
# Mehrfach ausführbar (idempotent), ohne Rückfragen.
#
# Alle Schritte sind an eine Datei geknüpft. Dadurch passt der Hook auf jeden
# Branch dieses Repos und bleibt gültig, wenn Zweige zusammengeführt werden.
set -euo pipefail

# Nur in der Remote-Umgebung ausführen; lokale Rechner bleiben unberührt.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

ROOT="${CLAUDE_PROJECT_DIR:-$(cd "$(dirname "$0")/../.." && pwd)}"
cd "$ROOT"

PIP_OPTS="--quiet --disable-pip-version-check --root-user-action=ignore"

# Python-Paket samt Entwicklungswerkzeugen (pytest) installieren.
# `-e` hält die Installation an den Arbeitsbaum gebunden: Codeänderungen wirken
# sofort, ohne erneute Installation.
if [ -f pyproject.toml ]; then
  echo "==> pip install -e .[dev]"
  # Einige Abhängigkeiten (z. B. cryptography) liegen im Container als
  # Debian-Paket unter /usr/lib/python3/dist-packages. Pip kann sie nicht
  # ersetzen („RECORD file not found") und bricht ab. --ignore-installed legt
  # dann eine eigene Kopie unter /usr/local ab, die beim Import Vorrang hat.
  if ! pip install $PIP_OPTS -e ".[dev]"; then
    echo "    Erstversuch fehlgeschlagen, weiche auf --ignore-installed aus"
    pip install $PIP_OPTS --ignore-installed -e ".[dev]"
  fi
elif [ -f requirements.txt ]; then
  echo "==> pip install -r requirements.txt"
  pip install $PIP_OPTS -r requirements.txt
fi

# literary_analysis.py braucht das Anthropic-SDK; es steht in keiner
# Paketliste, daher wird es bei Bedarf einzeln nachinstalliert.
if [ -f literary_analysis.py ] && ! python3 -c "import anthropic" >/dev/null 2>&1; then
  echo "==> pip install anthropic"
  pip install $PIP_OPTS anthropic
fi

# Node (Zweig „Stempeluhr"): legt node_modules an und zieht spätere
# Abhängigkeiten automatisch nach.
if [ -f package.json ]; then
  echo "==> npm install"
  npm install --no-audit --no-fund
fi

# Go (Zweig „Stempeluhr"): keine externen Module, nur Cache vorwärmen.
# `go test -run '^$'` übersetzt alles einschließlich der Testdateien, führt aber
# nichts aus und legt – anders als `go build` – keine Binärdatei im Arbeitsbaum ab.
if [ -f zeiterfassung/go.mod ]; then
  echo "==> go test/vet (Cache vorwärmen)"
  (cd zeiterfassung && go test -run '^$' ./... >/dev/null && go vet ./...)
fi

echo "==> Setup abgeschlossen"
