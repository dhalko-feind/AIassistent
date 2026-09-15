#!/usr/bin/env sh
# Baut die Stempeluhr als Windows-Programm (64 Bit) nach dist/Stempeluhr.exe.
# Voraussetzung: Go 1.24+. Es werden keine externen Module benötigt.
set -eu
cd "$(dirname "$0")"
mkdir -p dist
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -H windowsgui" -o dist/Stempeluhr.exe .
# Variante mit Konsolenfenster (zeigt Logausgaben, hilfreich bei Problemen)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w" -o dist/Stempeluhr-Konsole.exe .
ls -la dist/
