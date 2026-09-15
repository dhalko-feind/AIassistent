# Arbeitsanweisung – Automatische Nutzung des SOP-Skills „sop-claude-chatinterface“

**Bereich:** Marketing & Event Management · Fräsdienst-Service E. Feind GmbH  
**Verantwortlich:** David · **Version:** 1.0 · **Stand:** 2026-09-15 · **Review:** alle 6 Monate

---

## 1. Zweck

Diese Arbeitsanweisung beschreibt, **wann** der Skill `sop-claude-chatinterface`
automatisch greift und **wie** er im Arbeitsalltag anzuwenden ist. Ziel ist, dass
Marketing- und Event-Inhalte reproduzierbar, markenkonform und datenschutzsicher
entstehen – ohne dass die SOP-Regeln bei jeder Anfrage manuell wiederholt werden müssen.

Der Skill bündelt die verbindliche SOP v2.0 (siehe `SOP_Claude-Chatinterface_v2.md`)
in maschinenlesbarer, versionierter Form.

---

## 2. Wann der Skill automatisch aktiviert wird

Der Skill soll **ohne gesonderte Aufforderung** angewandt werden, sobald eine
dieser Situationen vorliegt:

- Erstellung von **Marketing-/Event-Inhalten**: LinkedIn-Post, E-Mail, Newsletter,
  Konzept, Pressemitteilung, Eventankündigung.
- **Prüfung oder Optimierung** eines vorhandenen Prompts oder Textentwurfs.
- Inhalte für **Industriekunden, Bestandskunden oder interne Kommunikation**.
- **Datei-Uploads**, die in einen Marketing-/Event-Kontext gehören.

Kurz: Immer wenn Text nach außen (Kunden, LinkedIn, Presse) oder nach innen (Team)
kommuniziert wird, gelten die SOP-Regeln.

---

## 3. Ablauf bei jeder Anfrage (5 Schritte)

| Schritt | Aktion |
|---------|--------|
| 1. Prüfen | Sind die Pflichtfelder da? (Rolle, Kontext, Aufgabe, Format, Länge, Zielgruppe, Tonalität, CTA, Muss-Inhalte, No-Gos). Optional per `scripts/prompt_optimizer.py`. |
| 2. Ergänzen | Fehlende Felder mit sinnvollen Defaults füllen **und transparent machen**, ODER max. 3 präzise Rückfragen stellen. |
| 3. Ausführen | Inhalt SOP-konform erstellen; bei kreativen Aufgaben 2–3 Varianten mit klarer Empfehlung. |
| 4. Prüfen | Fakten, Markenstimme, Rechtschreibung, Zielgruppenfit, rechtliche Unbedenklichkeit. |
| 5. Sichern | Ergebnis copy-paste-fähig ausgeben; Chat benennen nach `YYYY-MM-DD_Thema_Kanal`. |

**Optionale Prompt-Prüfung per Skript:**

```bash
python .claude/skills/sop-claude-chatinterface/scripts/prompt_optimizer.py "Dein Prompt-Text"
```

Das Skript prüft nur Format, Länge, Zielgruppe und CTA und schlägt Defaults vor –
es ruft keine API auf und schreibt keine Dateien.

---

## 4. Datenschutz & Compliance (nicht verhandelbar)

- **Keine** personenbezogenen Daten, Kundennamen, Preise, Verträge oder internen
  Strategien ohne ausdrückliche Freigabe.
- Vertrauliche Inhalte vor dem Upload anonymisieren.
- KI-Inhalte **vor jeder Veröffentlichung menschlich prüfen**; externe
  Veröffentlichung nur mit Freigabe des Verantwortlichen.
- KI-Kennzeichnungspflichten prüfen, wo relevant.
- Bei rechtlichen oder vertraglichen Fragen immer: **„Bitte Rechtsabteilung prüfen“**.

---

## 5. Ablage- & Deploy-Kontext

- Dieses Repository ist die **Single Source of Truth** für die SOP und den Skill:
  Änderungen an Regeln, Template oder Skript laufen über commit/PR und sind damit
  versioniert und nachvollziehbar.
- **Auslieferung:** Der Skill-Ordner `sop-claude-chatinterface/` wird in die
  Claude-Chat-/Projects-Umgebung des Marketing-Teams übernommen, wo die SOP inhaltlich gilt.
- **Geltungsbereich der SOP:** ausschließlich das Claude-Chatinterface – **nicht**
  Claude Code, API oder Automatisierungen (siehe SOP §2).
- **Pflege:** David · Review spätestens alle 6 Monate oder bei Prozessänderungen.

---

## 6. Verweise

- Vollständiges Regelwerk: `SOP_Claude-Chatinterface_v2.md`
- Skill-Definition & Kernregeln: `../SKILL.md`
- Prompt-Prüfung: `../scripts/prompt_optimizer.py`
