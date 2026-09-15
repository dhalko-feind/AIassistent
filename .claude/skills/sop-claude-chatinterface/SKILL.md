---
name: sop-claude-chatinterface
description: >
  Standard Operating Procedure für die Nutzung des Claude-Chatinterfaces
  im Bereich Marketing & Event Management der Fräsdienst-Service E. Feind GmbH.
  Verwende diesen Skill immer, wenn der Nutzer Marketing-, Event-, Social-Media-,
  E-Mail-, Pressemitteilungs-, Konzept- oder LinkedIn-Inhalte erstellt, prüft,
  optimiert oder Prompts für Claude formuliert. Der Skill prüft Prompts gegen
  die SOP, ergänzt fehlende Pflichtfelder (Format, Länge, Zielgruppe, CTA),
  wahrt Markenstimme und Datenschutz und liefert direkt verwendbare Ergebnisse.
license: Proprietär – Fräsdienst-Service E. Feind GmbH
---

# SOP – Nutzung des Claude-Chatinterfaces (Marketing & Event Management)

Diese Skill bündelt die verbindlichen Regeln für die Arbeit mit Claude im
Chatinterface. Sie gilt **nur** für den Chatmodus (nicht für Claude Code, API
oder Automatisierungen).

Verantwortlich: David · Überprüfung: alle 6 Monate · Version: 2.0

**Weiterführende Dateien:**
- Vollständiges Regelwerk: `references/SOP_Claude-Chatinterface_v2.md`
- Arbeitsanweisung (Auto-Skill-Nutzung, Deploy-Kontext): `references/Arbeitsanweisung.md`
- Pflichtfeld-Prüfung als Skript: `scripts/prompt_optimizer.py`

---

## Wann dieser Skill greift

Verwende ihn automatisch, sobald eine der folgenden Situationen eintritt:

- Der Nutzer formuliert einen Marketing-Prompt (LinkedIn, E-Mail, Konzept,
  Pressemitteilung, Eventankündigung, Newsletter).
- Der Nutzer bittet um Prüfung oder Optimierung eines Prompts.
- Der Nutzer will Inhalte für Industriekunden, Bestandskunden oder interne
  Kommunikation erstellen.
- Der Nutzer lädt Dateien hoch, die in einen Chat-Kontext gehören.

---

## Kernregeln (immer anwenden)

### 1. Ein Thema = ein Chat
- Neue Aufgabe → neuer Chat.
- Themenwechsel, Zielwechsel oder nach ~10–15 Nachrichten → neuer Chat.
- Bei wiederkehrenden Themen: Claude Projects nutzen.
- Wenn nötig, eine kurze Zusammenfassung des Vorergebnisses als Startkontext.

### 2. Pflichtfelder einer Anfrage
Jede Anfrage **muss** mindestens enthalten:

| Feld          | Beispiel                                          |
|---------------|---------------------------------------------------|
| Rolle         | „Du bist Marketingmanager im Industrieumfeld.“    |
| Kontext       | Anlass, Produkt, Veranstaltung, Ziel              |
| Aufgabe       | LinkedIn-Post, E-Mail, Konzept, PM                |
| Format        | Fließtext, Bullets, Tabelle, Markdown             |
| Länge         | 80–100 Wörter / max. 1 DIN-A4-Seite               |
| Zielgruppe    | Industriekunden, Bestandskunden, intern           |
| Tonalität     | professionell, locker, technisch, vertrieblich    |
| CTA           | falls relevant                                    |
| Muss-Inhalte  | Datum, Ort, Link                                  |
| No-Gos        | keine Superlative, keine Preise                   |
| Quellen       | falls aktuelle Infos nötig                        |

Fehlt etwas, **frage max. 3 präzise Rückfragen** ODER ergänze sinnvolle
Defaults (siehe Prompt-Template) und weise den Nutzer darauf hin.

### 3. Rollen- und Kontextsteuerung
- Klare Rolle zuweisen.
- Nur relevanten Kontext liefern.
- Bei kreativen Aufgaben: **2–3 Varianten** mit klarer Empfehlung.

### 4. Ausgabeformat
- Format explizit vorgeben (Text, Tabelle, Konzept-Struktur).
- Vor Verwendung immer prüfen auf: Fakten, Markenstimme, Rechtschreibung,
  Zielgruppenfit, rechtliche Unbedenklichkeit.

### 5. Dateiupload
- Nur notwendige Dateien hochladen.
- Keine Ordner/Sammeluploads.
- Keine vertraulichen/personenbezogenen Daten ohne Freigabe.
- Vertrauliches vorher anonymisieren.

### 6. Korrekturen & Iteration
- Ursprüngliche Nachricht **bearbeiten**, statt neue unpräzise Nachricht.
- Konkret formulieren: „Kürze auf 80 Wörter.“ / „Tonalität sachlicher.“
- Neue Themen → neuer Chat.

### 7. Sprache & Tonalität
- Grundsätzlich **Deutsch**.
- Englische Fachbegriffe nur, wenn im Unternehmen üblich.
- Keine diskriminierenden, irreführenden oder rechtlich problematischen Formulierungen.

### 8. Datenschutz & Compliance
- Keine personenbezogenen Daten, Kundennamen, Preise, Verträge oder internen
  Strategien ohne Freigabe.
- KI-Inhalte vor Veröffentlichung immer menschlich prüfen.
- Externe Veröffentlichung nur mit Freigabe des Verantwortlichen.
- KI-Kennzeichnungspflichten prüfen.
- Bei rechtlichen/vertraglichen Fragen immer: „Bitte Rechtsabteilung prüfen“.

### 9. Websuche
- Nur bei Bedarf an aktuellen Informationen.
- Quellen, Datum und Links immer mitliefern lassen.
- Quellen kritisch prüfen.

---

## Vorgehen bei jeder Anfrage

1. **Prüfen**: Enthält der Prompt die Pflichtfelder? (siehe `scripts/prompt_optimizer.py`)
2. **Ergänzen**: Fehlende Felder mit sinnvollen Defaults auffüllen und transparent machen.
3. **Ausführen**: Inhalt gemäß SOP erstellen.
4. **Prüfen**: Fakten, Markenstimme, Recht, Zielgruppe.
5. **Sichern**: Ergebnis sofort copy-paste-fähig ausgeben; Chat benennen
   nach Schema `YYYY-MM-DD_Thema_Kanal`.

Die Pflichtfeld-Prüfung lässt sich optional per Skript unterstützen:

```bash
python scripts/prompt_optimizer.py "Dein Prompt-Text hier"
```

Das Skript prüft nur (Format, Länge, Zielgruppe, CTA) und schlägt Defaults vor –
es ruft keine API auf und schreibt keine Dateien.

---

## Prompt-Template (immer bevorzugen)

```text
Rolle: Du bist [Rolle].
Kontext: [Anlass, Produkt, Veranstaltung, Hintergrund].
Aufgabe: [Was genau soll erstellt werden?]
Zielgruppe: [Industriekunden / Bestandskunden / intern].
Format: [LinkedIn-Post / E-Mail / Konzept / Tabelle].
Länge: [80–100 Wörter / max. 1 Seite].
Tonalität: [professionell / sachlich / vertrieblich].
Muss-Inhalte: [Datum, Ort, CTA-Link].
No-Gos: [keine Superlative, keine Preise].
Call-to-Action: [falls relevant].
Quellen: [falls aktuelle Infos nötig].
```
