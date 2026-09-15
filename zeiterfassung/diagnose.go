package main

// diagnose.go – Diagnosebericht über den OptiTime-Ordner: welche Dateien liegen dort,
// welche Formate wurden erkannt, wie sehen die ersten Zeilen bzw. Tabellen aus.
// Der Bericht enthält Auszüge der eigenen Daten und ist zum Weitergeben an den
// Entwickler gedacht.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	diagMaxFiles = 400
	diagMaxLines = 6
	diagMaxCell  = 60
)

func trunc(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", "⏎")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (a *App) diagnose() string {
	a.mu.Lock()
	id := a.identityWithProfile()
	sync := a.sync
	explicit, configured := a.explicit, a.cfg.OptiTimePath
	a.mu.Unlock()

	var b strings.Builder
	w := func(f string, args ...interface{}) { fmt.Fprintf(&b, f+"\n", args...) }
	w("Stempeluhr %s – Diagnose vom %s", version, time.Now().Format("02.01.2006 15:04:05"))
	w("Benutzer: %s\\%s (%s) auf %s, Quelle: %s", id.Domain, id.Username, id.FullName, id.Host, id.Source)
	w("Profil: %s", id.ProfileDir)
	w("Kennungen: %s | bestätigt: %s", strings.Join(id.Keys, "; "), strings.Join(id.ConfirmedKeys, "; "))
	path, source, searched := findOptiTimePath(explicit, configured, id.ProfileDir)
	w("OptiTime-Ordner: %q (%s)", path, source)
	w("Geprüfte Orte (%d): %s", len(searched), strings.Join(searched, " | "))
	w("Letzter Abgleich: %s, %d Buchungen, Zuordnung %q (automatisch: %v), Fehler: %s",
		sync.At.Format("15:04:05"), sync.Imported, sync.Identity.MatchedPerson, sync.Identity.AutoMatched, strings.Join(sync.Errors, "; "))
	if path == "" {
		return b.String()
	}

	w("")
	w("== Dateien unter %s ==", path)
	var all []string
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			w("  Fehler: %s: %v", p, err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		all = append(all, p)
		if len(all) >= diagMaxFiles {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Strings(all)
	for _, p := range all {
		rel, _ := filepath.Rel(path, p)
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		magic := fileMagic(p)
		if magic == "" {
			magic = "-"
		}
		w("  %-60s %10d B  %s  [%s]", rel, info.Size(), info.ModTime().Format("02.01.2006 15:04"), magic)
	}
	if len(all) >= diagMaxFiles {
		w("  … (Liste bei %d Dateien abgeschnitten)", diagMaxFiles)
	}

	w("")
	w("Stammdaten: %d Mitarbeiter, %d Tätigkeiten, %d Aufträge (Pause=%q, Kommen=%q)",
		sync.Master.Persons, sync.Master.Activities, sync.Master.Orders, sync.Master.PauseActivity, sync.Master.WorkActivity)
	w("")
	w("== Inhalt der Datendateien ==")
	files, other := listFiles(path)
	if len(other) > 0 {
		var parts []string
		for k, v := range other {
			parts = append(parts, fmt.Sprintf("%s ×%d", k, v))
		}
		sort.Strings(parts)
		w("Nicht gelesene Typen: %s", strings.Join(parts, ", "))
	}
	master := loadMasterFrom(files)
	for _, f := range files {
		rel, _ := filepath.Rel(path, f)
		w("")
		w("-- %s", rel)
		raw, err := os.ReadFile(f)
		if err != nil {
			w("  Lesefehler: %v", err)
			continue
		}
		if isSQLite(raw) {
			a.diagnoseSQLite(&b, f)
			continue
		}
		text := decodeText(raw)
		lines := strings.Split(text, "\n")
		w("  Zeichensatz: %s, Zeilen: %d", encodingName(raw), len(lines))
		kind := classifyOptiFile(f, text)
		if label, ok := master.files[f]; ok {
			w("  Art: %s, %d Einträge übernommen", label, master.rows[f])
			for i := 0; i < len(lines) && i < diagMaxLines; i++ {
				w("  %2d| %s", i+1, trunc(redactOffliste(redactLine(lines[i]), kind), 200))
			}
			continue
		}
		if kind == "log" {
			w("  Art: OptiTime-Protokoll")
			entries, _, perr := parseFile(f, master)
			if perr != nil {
				w("  Erkennung: %v", perr)
			} else {
				w("  %d Stempelereignisse erkannt", len(entries))
				for i := 0; i < len(entries) && i < 3; i++ {
					e := entries[i]
					w("  -> %s %s Tätigkeit=%s (%s) Typ=%d Person=%q", e.date, e.start, e.order, e.activity, e.optiKind, e.person)
				}
				if len(entries) > 0 {
					last := entries[len(entries)-1]
					w("  -> … letztes: %s %s Tätigkeit=%s (%s)", last.date, last.start, last.order, last.activity)
				}
			}
			for i := 0; i < len(lines) && i < 3; i++ {
				w("  %2d| %s", i+1, trunc(lines[i], 200))
			}
			continue
		}
		w("  Trennzeichen: %q", string(detectDelimiter(lines[0])))
		for i := 0; i < len(lines) && i < diagMaxLines; i++ {
			w("  %2d| %s", i+1, trunc(redactLine(lines[i]), 200))
		}
		entries, format, perr := parseFile(f, master)
		if perr != nil {
			w("  Erkennung: %s – %v", format, perr)
		} else {
			w("  Erkennung: %s, %d Datensätze", format, len(entries))
			for i := 0; i < len(entries) && i < 3; i++ {
				e := entries[i]
				w("  -> %s %s-%s person=%q event=%q order=%q activity=%q", e.date, e.start, e.end, e.person, e.event, e.order, e.activity)
			}
		}
	}
	return b.String()
}

// encodingName benennt den erkannten Zeichensatz einer Textdatei.
func encodingName(raw []byte) string {
	switch {
	case len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF:
		return "utf-8 (BOM)"
	case len(raw) >= 2 && ((raw[0] == 0xFF && raw[1] == 0xFE) || (raw[0] == 0xFE && raw[1] == 0xFF)):
		return "utf-16"
	case !isValidUTF8(raw):
		return "windows-1252 (angenommen)"
	}
	return "utf-8"
}

func (a *App) diagnoseSQLite(b *strings.Builder, path string) {
	w := func(f string, args ...interface{}) { fmt.Fprintf(b, f+"\n", args...) }
	db, err := openSQLite(path)
	if err != nil {
		w("  SQLite: %v", err)
		return
	}
	w("  SQLite: Seitengröße %d, Seiten %d, Kodierung %d", db.pageSize, db.pages, db.encoding)
	if _, err := os.Stat(path + "-wal"); err == nil {
		w("  Hinweis: WAL-Datei vorhanden – neueste Buchungen können noch dort liegen.")
	}
	tables, err := db.tables()
	if err != nil {
		w("  Schema nicht lesbar: %v", err)
		return
	}
	for _, t := range tables {
		rows, rerr := db.rows(t, 0)
		w("  Tabelle %s (%d Zeilen): %s", t.Name, len(rows), strings.Join(t.Columns, ", "))
		w("    %s", trunc(t.SQL, 300))
		if rerr != nil {
			w("    Lesefehler: %v", rerr)
		}
		for i := 0; i < len(rows) && i < 3; i++ {
			var parts []string
			for _, c := range t.Columns {
				parts = append(parts, c+"="+trunc(rows[i][c], diagMaxCell))
			}
			w("    %s", strings.Join(parts, " | "))
		}
		if len(rows) > 3 {
			last := rows[len(rows)-1]
			var parts []string
			for _, c := range t.Columns {
				parts = append(parts, c+"="+trunc(last[c], diagMaxCell))
			}
			w("    … letzte: %s", strings.Join(parts, " | "))
		}
	}
}

var secretKeyRe = regexp.MustCompile(`(?i)^([A-Za-z_][A-Za-z0-9_()]*(pw|passwor|kennwort|pin|secret|token|user)[A-Za-z0-9_()]*) *=(.*)$`)

// redactLine entfernt Zugangsdaten aus Diagnosezeilen: Schlüssel wie SMTPPW
// oder SUPERPIN in config.ini und die Kennwortspalte der Mitarbeiterliste.
func redactLine(line string) string {
	if m := secretKeyRe.FindStringSubmatch(line); m != nil {
		if strings.TrimSpace(m[3]) == "" {
			return line
		}
		return m[1] + "=<entfernt>"
	}
	return line
}

func isValidUTF8(b []byte) bool {
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			i++
			continue
		}
		var n int
		switch {
		case c&0xE0 == 0xC0:
			n = 1
		case c&0xF0 == 0xE0:
			n = 2
		case c&0xF8 == 0xF0:
			n = 3
		default:
			return false
		}
		if i+n >= len(b) {
			return false
		}
		for k := 1; k <= n; k++ {
			if b[i+k]&0xC0 != 0x80 {
				return false
			}
		}
		i += n + 1
	}
	return true
}
