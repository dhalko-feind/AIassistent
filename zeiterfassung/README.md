# Stempeluhr – Zeiterfassung

Weiterentwicklung der Google-Tabelle „Zeiterfassung_David-Halko" zu einer eigenständigen
Zeiterfassung. Zwei Betriebsarten:

| Betriebsart | Start | Datenablage | OptiTime |
| --- | --- | --- | --- |
| **Windows-Programm** `Stempeluhr.exe` | Doppelklick | je Benutzer unter `%APPDATA%\Stempeluhr\<Benutzer>\data.json`, Ordner frei wählbar | wird bei jedem Start gesucht und eingelesen |
| **HTML-Seite** `index.html` | im Browser öffnen | Browser (localStorage) | nur per CSV-Import |

## Windows-Programm (EXE)

`Stempeluhr.exe` ist eine einzelne Datei ohne Installation. Beim Start:

1. **Benutzer festlegen**: Standard ist der angemeldete Windows-Benutzer. Mit dem Startparameter
   `--user d.halko` oder der Umgebungsvariable `STEMPELUHR_USER=d.halko` wird der Benutzer als
   Variable gesetzt, z. B. in einer Verknüpfung oder Batch-Datei. Daraus ergibt sich das Profil
   `C:\Users\d.halko`, auch wenn das Programm unter einem anderen Konto läuft.
2. **OptiTime-Ordner suchen**, in dieser Reihenfolge: Startparameter `--optitime`,
   Umgebungsvariable `OPTITIME_PATH`, gespeicherter Pfad aus den Einstellungen, dann
   **`C:\Users\<user>\AppData\Local\OptiTime`** (sowie `AppData\Roaming\OptiTime`,
   `AppData\LocalLow\OptiTime` und abweichend geschriebene Ordner wie „Opti-Time" unter
   `AppData\Local`), zuletzt eine allgemeine Suche unter OneDrive, Benutzerprofil, `%ProgramData%`,
   `%ProgramFiles%`, `%ProgramFiles(x86)%`, `%PUBLIC%` und allen lokalen Festplatten.
3. **Zeitstempeldaten einlesen.** Vorrang hat das Terminal-Protokoll des OptiTime-Clients:

   | Datei | Inhalt | Verwendung |
   | --- | --- | --- |
   | `logger.txt` | Protokoll mit allen Stempelvorgängen | Quelle der Buchungen |
   | `Offliste*.txt` | Stammdaten, µ-getrennt | Namen, Tätigkeiten, Aufträge |
   | `config.ini` | Terminal-Einstellungen | Tätigkeitsnummer für Pause und Kommen |

   Eine Buchung steht im Protokoll als `VerarbeiteBuchung: PNR=1626 KST=000007`, maßgeblich ist
   der Zeitstempel der Protokollzeile (`BEG_STD`/`BEG_MIN` enthalten beide die Stunde und sind
   als Uhrzeit unbrauchbar). Übernommen wird nur, was das Terminal bestätigt hat
   („hat an-/um-/ausgestempelt"); abgewiesene Versuche („ist noch gar nicht da") werden übersprungen.
   Die Tätigkeitsnummer bestimmt den Typ: `000999` Kommen, `000007` Pause, `000000` Arbeitsende.
   Namen und Bezeichnungen kommen aus `OfflisteMa.txt`, `OfflisteTaet.txt` und `OfflisteAuf.txt`;
   fehlen sie, gelten die Standardnummern.

   Daneben werden weiterhin gelesen: `.csv`, `.txt`, `.tsv`, `.json`, `.xml`, `.log`, `.dat`,
   `.asc`, `.ini` und SQLite-Datenbanken (Endung beliebig, erkannt am Dateikopf), bis sechs Ebenen
   tief. Zeichensätze UTF-8 (mit/ohne BOM), UTF-16 und Windows-1252. Verstanden werden dort:
   - **Intervalle**: `Datum`, `von`, `bis` (+ optional `Auftrag`, `Tätigkeit`, `Zeitart`, `Bemerkung`).
   - **Stempelereignisse**: `Datum`, `Uhrzeit` (oder `Zeitstempel`) und `Buchung`/`Buchungsart`.
   - Beides gemischt, als CSV-Spalten, JSON-Felder, XML-Attribute/-Elemente oder Tabellenspalten
     einer SQLite-Datenbank. Zahlen-Zeitstempel (Unix-Sekunden/-Millisekunden, .NET-Ticks,
     OLE-Datum) werden umgerechnet; Mitarbeiter-IDs über eine Stammdatentabelle zum Namen aufgelöst.
4. **Nur eigene Daten übernehmen**:
   - Hat die Datei eine Personenspalte, werden nur Zeilen übernommen, die zum Benutzer
     (Anmeldename, Anzeigename), zur Personalnummer oder zum in den Einstellungen bestätigten
     Namen passen. Zeilen anderer Personen werden gezählt, aber nie importiert.
   - Ist die Zuordnung nur automatisch über den Windows-Namen erfolgt, zeigt die App
     „automatisch erkannt – bitte bestätigen". Mit „Das bin ich" wird sie fest gespeichert.
   - Dateien **ohne** Personenspalte gelten als eigene, wenn sie im Profil des Benutzers liegen
     (`C:\Users\<user>\…`) oder Dateiname bzw. Ordner den Benutzer nennen. Andernfalls erscheinen
     sie als „nicht zugeordnet".
5. **Oberfläche öffnen**: lokaler Server nur auf `127.0.0.1` mit freiem Port, Standardbrowser
   startet automatisch. Wird das Browserfenster geschlossen, beendet sich das Programm nach
   zwei Minuten von selbst; „Stempeluhr beenden" beendet sofort.

OptiTime ist führend: Bei jedem Abgleich ersetzt eine OptiTime-Buchung eine vorhandene Buchung
mit gleichem Datum und gleicher Startzeit. Manuelle Buchungen bleiben sonst erhalten.

### Grenzen des Protokolls

Das Protokoll enthält nur, was an **diesem** Rechner gestempelt wurde. Schließt der OptiTime-Server
einen Tag automatisch ab („Arbeitsende vom System"), ohne dass am Terminal gestempelt wurde, fehlt
dieses Ereignis lokal: Die letzte Buchung des Tages bleibt offen und wird als „nicht ausgestempelt"
angezeigt. Die Endzeit lässt sich im Reiter „Buchungen" nachtragen. Die maßgebliche Abrechnung
liegt weiterhin auf dem Server (`\\S21\OptiControl` laut `config.ini`).

### Diagnose

Wenn nichts oder das Falsche eingelesen wird: In der OptiTime-Karte auf **Diagnose** klicken
(oder `Stempeluhr.exe --user d.halko --diagnose` starten). Der Bericht listet alle Dateien im
OptiTime-Ordner mit Größe, Datum und erkanntem Typ, zeigt die ersten Zeilen der Textdateien bzw.
Tabellen, Spalten und Beispielzeilen der Datenbanken und die Erkennung je Datei. Er liegt unter
`%APPDATA%\Stempeluhr\diagnose.txt` und enthält Auszüge der eigenen Zeitdaten. Zugangsdaten werden
entfernt: Konfigurationsschlüssel mit `PW`, `PASSWORT`, `KENNWORT`, `PIN`, `SECRET`, `TOKEN` oder
`USER` im Namen sowie die Kennwortspalte der Mitarbeiterliste erscheinen als `<entfernt>`.

### Startparameter

```
Stempeluhr.exe --user d.halko                            Benutzer als Variable setzen (auch STEMPELUHR_USER)
Stempeluhr.exe --optitime "\\server\OptiTime\Export"   OptiTime-Ordner fest vorgeben
Stempeluhr.exe --data "%OneDrive%\Stempeluhr"            Datenordner vorgeben (sonst in der Oberfläche einstellbar)
Stempeluhr.exe --port 8123 --no-browser                  fester Port, Browser nicht öffnen
Stempeluhr.exe --idle 0                                  nie automatisch beenden
Stempeluhr.exe --diagnose                                Diagnosebericht schreiben und beenden
```

Protokoll: `%APPDATA%\Stempeluhr\stempeluhr.log`. Einstellungen: `%APPDATA%\Stempeluhr\config.json`.
`Stempeluhr-Konsole.exe` ist dieselbe App mit sichtbarem Konsolenfenster für die Fehlersuche.

Beispiel-Verknüpfung (Ziel): `"C:\Tools\Stempeluhr.exe" --user d.halko`

### Datenablage

Standard ist `%APPDATA%\Stempeluhr\<Benutzer>\data.json`. Der Ordner lässt sich in der Oberfläche
unter „Daten & Einstellungen" → **Datenablage** ändern: Pfad eintragen oder einen der Vorschläge
(OneDrive, Dokumente, Standard) anklicken, dann „Speichern & übernehmen". `%VARIABLE%` im Pfad wird
aufgelöst. Beim Wechsel gilt:

- Vorhandene Buchungen werden in den neuen Ordner übernommen.
- Liegt dort bereits eine `data.json`, werden beide zusammengeführt (gleiches Datum und gleiche
  Startzeit zählt als dieselbe Buchung).
- Die bisherige Datei bleibt als Sicherung liegen und wird nicht gelöscht.
- Der Ordner wird in `config.json` gespeichert und beim nächsten Start wieder verwendet.

Im gewählten Ordner wird je Benutzer ein Unterordner angelegt, damit sich mehrere Personen einen
Ordner (Netzlaufwerk, gemeinsames Laufwerk) teilen können, ohne sich zu überschreiben.
„Ordner öffnen" startet den Explorer im Datenordner.

### Mehrere Geräte

Die EXE ist portabel (z. B. auf einem USB-Stick oder Netzlaufwerk). Auf jedem Gerät gilt der dort
angemeldete Benutzer. Sollen manuelle Buchungen auf allen Geräten gleich sein, den Datenordner auf
einen OneDrive-Ordner legen – entweder in der Oberfläche oder per `--data`. OptiTime wird auf jedem
Gerät weiterhin lokal gelesen.

### Bauen

```bash
./build.sh          # Tests + Windows-Build nach dist/ (Go 1.24+, keine externen Module)
```

## Funktionen der Oberfläche

- **Stempeluhr**: Kommen · Pause · Weiter · Gehen mit Live-Uhr und Status.
- **Tagesübersicht**: Einstempelzeit, Ausstempelzeit, Pausen, Netto/Brutto, KW, Tages-Soll, Überstunden.
- **Buchungen**: alle Einzelbuchungen (Auftrag 000999 „Kommen" / 000007 „*Pause*"), bearbeiten, nachtragen,
  löschen, Monatsfilter; OptiTime-Buchungen sind markiert.
- **Wochen**: Ist vs. Soll je Tag als Balken, Wochensumme, Überstunden.
- **Hinweise nach ArbZG**: Pause < 30 min bei > 6 h, < 45 min bei > 9 h, > 10 h/Tag, nicht ausgestempelte Buchungen.
- **Import/Export**: CSV (Semikolon, Excel-tauglich) für Buchungen und Tage, JSON-Backup, Import aus CSV oder JSON.
- **Einstellungen**: Wochenstunden und Arbeitstage → Tages-Soll.

In der HTML-Betriebsart sind die Buchungen vom 17.08. bis 07.09.2026 aus der Google-Tabelle vorgeladen
(`seed-data.js`); in der EXE lassen sie sich über „Daten aus Google-Tabelle laden" nachladen.

## Dateien

| Datei | Zweck |
| --- | --- |
| `main.go` | Windows-Programm: Benutzer, lokaler Server, Datenablage |
| `optitime.go` | OptiTime-Pfadsuche, Dateiformate, Zuordnung zum Benutzer |
| `optilog.go` | Terminal-Protokoll, Offlisten und config.ini |
| `sqlite.go` | Lesender SQLite-Zugriff ohne externe Module |
| `diagnose.go` | Diagnosebericht über den OptiTime-Ordner |
| `sys_windows.go` / `sys_other.go` | Plattformteile (Laufwerke, Browser, Meldungsfenster) |
| `index.html`, `app.js` | Oberfläche (wird in die EXE eingebettet) |
| `zeit.js` | Reine Berechnungslogik (Browser und Node) |
| `seed-data.js` | Startdaten aus der Google-Tabelle |
| `test/zeit.test.js`, `optitime_test.go` | Tests (`npm test`, `go test ./...`) |

## Annahmen

- Tages-Soll = Wochenstunden ÷ Anzahl Arbeitstage (Standard 40 h / 5 Tage = 8:00 h), nur für Tage mit Buchungen.
- Urlaub, Krankheit und Feiertage werden nicht erfasst.
- Offene Buchungen zählen nur am heutigen Tag (bis jetzt) in die Arbeitszeit.
- Das Protokollformat wurde aus dem Protokoll des Terminals NB-EFEIND-0021 (Stand 11.09.2026) abgeleitet.
  Ändert ein OptiTime-Update die Meldungstexte, meldet die Diagnose „keine Buchungen im Protokoll gefunden".
- Excel-Dateien (`.xlsx`), Access-Datenbanken und verschlüsselte Datenbanken werden nicht gelesen;
  sie erscheinen im Diagnosebericht.

Die ArbZG-Hinweise sind Orientierung, keine Rechtsberatung. Bei Fragen zur betrieblichen Arbeitszeitregelung
bitte Rechtsabteilung prüfen.
