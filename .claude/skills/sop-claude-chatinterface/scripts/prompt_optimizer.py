#!/usr/bin/env python3
"""Prompt-Validierung für die Marketing-SOP (Claude-Chatinterface).

Prüft einen Prompt gegen die SOP-Pflichtfelder (Format, Länge, Zielgruppe, CTA)
und schlägt sinnvolle Defaults für fehlende Felder vor.

SOP-konform: reine Prüf-/Vorschlagslogik – kein API-Aufruf, keine Datei- oder
Ordner-Operationen, keine externen Abhängigkeiten (nur Standardbibliothek).

Nutzung:
    python prompt_optimizer.py "Dein Prompt-Text hier"
"""

import re
import sys

# Pflichtfelder gemäß SOP (siehe SKILL.md, Abschnitt "Pflichtfelder einer Anfrage")
REQUIRED_FIELDS = ["format", "länge", "zielgruppe", "cta"]


def validate_prompt(prompt: str) -> dict:
    """Prüft heuristisch, ob die Pflichtfelder im Prompt genannt sind."""
    prompt_lower = prompt.lower()
    results = {}

    formats = ["linkedin", "e-mail", "email", "konzept", "post", "text",
               "pressemitteilung", "newsletter", "tabelle"]
    results["format"] = any(f in prompt_lower for f in formats)

    results["länge"] = bool(re.search(r"\d+\s*[-–]\s*\d+\s*wörter", prompt_lower)) \
        or bool(re.search(r"max\.?\s*\d+", prompt_lower)) \
        or "seite" in prompt_lower

    results["zielgruppe"] = "zielgruppe" in prompt_lower or \
        any(z in prompt_lower for z in ["kunden", "industrie", "team", "partner", "intern"])

    results["cta"] = any(c in prompt_lower for c in
                         ["cta", "call-to-action", "link", "registrierung", "anmeldung"])

    return results


def optimize_prompt(prompt: str, validation: dict) -> tuple:
    """Ergänzt fehlende Felder mit sinnvollen Defaults (einfache Heuristik)."""
    additions = []

    if not validation["format"]:
        additions.append("Format: LinkedIn-Post")
    if not validation["länge"]:
        additions.append("Länge: 80–100 Wörter")
    if not validation["zielgruppe"]:
        additions.append("Zielgruppe: Industriekunden")
    if not validation["cta"]:
        additions.append("CTA: Registrierungslink")

    if additions:
        optimized = prompt.strip() + " | " + " | ".join(additions)
    else:
        optimized = prompt.strip()

    return optimized, additions


def print_validation(results: dict) -> None:
    """Gibt das Prüfergebnis übersichtlich auf der Konsole aus."""
    print("\n--- PROMPT-VALIDIERUNG ---")
    for field, ok in results.items():
        status = "OK" if ok else "FEHLT"
        print(f"{field.capitalize():12}: {status}")
    print("--------------------------\n")


def main() -> None:
    if len(sys.argv) < 2:
        print('Nutzung: python prompt_optimizer.py "PROMPT-TEXT"')
        sys.exit(1)

    prompt = sys.argv[1]

    # 1) Prompt prüfen
    results = validate_prompt(prompt)
    print_validation(results)

    # 2) Prompt optimieren
    optimized_prompt, additions = optimize_prompt(prompt, results)
    if additions:
        print("Automatische Ergänzungen:")
        for a in additions:
            print(f"- {a}")
        print("\nOptimierter Prompt:")
        print(optimized_prompt)
    else:
        print("Prompt bereits vollständig gemäß SOP.")


if __name__ == "__main__":
    main()
