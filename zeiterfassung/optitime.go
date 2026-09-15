package main

// optitime.go – Auffinden des OptiTime-Pfads, Einlesen der Exportdateien
// und Zuordnung der Buchungen zum angemeldeten Benutzer.

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// ---------- Ergebnis-Strukturen ----------

type Identity struct {
	Username      string   `json:"username"`      // Windows-Anmeldename ohne Domäne
	Domain        string   `json:"domain"`        // Domäne/Rechner
	FullName      string   `json:"fullName"`      // Anzeigename aus dem Konto
	Host          string   `json:"host"`          // Rechnername
	Source        string   `json:"source"`        // "windows", "parameter" (--user) oder "umgebung" (STEMPELUHR_USER)
	ProfileDir    string   `json:"profileDir"`    // C:\Users\<Benutzer>
	Keys          []string `json:"keys"`          // alle Kennungen, die als "ich" gelten
	ConfirmedKeys []string `json:"confirmedKeys"` // vom Benutzer bestätigte Kennungen (Profil)
	MatchedPerson string   `json:"matchedPerson"` // in den Dateien gefundene Person, die als "ich" gilt
	AutoMatched   bool     `json:"autoMatched"`   // Zuordnung automatisch (true) oder manuell bestätigt (false)
}

type FileResult struct {
	Path    string `json:"path"`
	Format  string `json:"format"` // "intervall", "ereignisse", "json", "unbekannt"
	Rows    int    `json:"rows"`
	Taken   int    `json:"taken"`   // dem Benutzer zugeordnete Buchungen
	Skipped int    `json:"skipped"` // Zeilen anderer Personen
	Reason  string `json:"reason,omitempty"`
}

// MasterInfo fasst die eingelesenen Stammdaten zusammen.
type MasterInfo struct {
	Persons       int    `json:"persons"`
	Activities    int    `json:"activities"`
	Orders        int    `json:"orders"`
	PauseActivity string `json:"pauseActivity"`
	WorkActivity  string `json:"workActivity"`
}

type SyncResult struct {
	At         time.Time      `json:"at"`
	Path       string         `json:"path"`
	PathSource string         `json:"pathSource"` // "parameter", "umgebung", "konfiguration", "gefunden", ""
	Searched   []string       `json:"searched"`
	Files      []FileResult   `json:"files"`
	Persons    []string       `json:"persons"` // in den Dateien gefundene Personen
	Identity   Identity       `json:"identity"`
	Imported   int            `json:"imported"`
	Unassigned int            `json:"unassigned"` // Dateien ohne Personenspalte, die nicht zugeordnet werden konnten
	OtherFiles map[string]int `json:"otherFiles"` // im Ordner gefundene, nicht gelesene Dateitypen (Endung -> Anzahl)
	Master     MasterInfo     `json:"master"`     // eingelesene Stammdaten des Terminals
	Errors     []string       `json:"errors"`
}

// ---------- Pfadsuche ----------

var optiNameRe = regexp.MustCompile(`(?i)opti[\s_-]*time`)

// findOptiTimePath sucht den OptiTime-Ordner. Reihenfolge: expliziter Pfad,
// Umgebungsvariable OPTITIME_PATH, Konfiguration, dann das Profil des Benutzers
// (C:\Users\<Benutzer>\AppData\Local\OptiTime), zuletzt bekannte Orte.
func findOptiTimePath(explicit, configured, profile string) (path, source string, searched []string) {
	try := func(p, src string) bool {
		if p == "" {
			return false
		}
		searched = append(searched, p)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			path, source = p, src
			return true
		}
		return false
	}
	if try(explicit, "parameter") {
		return
	}
	if try(os.Getenv("OPTITIME_PATH"), "umgebung") {
		return
	}
	if try(configured, "konfiguration") {
		return
	}
	if profile != "" {
		for _, rel := range []string{`AppData\Local\OptiTime`, `AppData\Local\Optitime`, `AppData\Roaming\OptiTime`, `AppData\LocalLow\OptiTime`, `.local/share/OptiTime`} {
			if try(filepath.Join(profile, filepath.FromSlash(strings.ReplaceAll(rel, `\`, "/"))), "benutzerprofil") {
				return
			}
		}
		// Ordnername abweichend geschrieben (z. B. "Opti-Time"): unter AppData\Local suchen
		local := filepath.Join(profile, "AppData", "Local")
		searched = append(searched, local)
		if p := scanForOptiDir(local, 2); p != "" {
			path, source = p, "benutzerprofil"
			return
		}
	}
	for _, base := range candidateBases() {
		searched = append(searched, base)
		if p := scanForOptiDir(base, 2); p != "" {
			path, source = p, "gefunden"
			return
		}
	}
	return "", "", searched
}

// candidateBases liefert die Orte, an denen OptiTime üblicherweise liegt.
func candidateBases() []string {
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		for _, o := range out {
			if strings.EqualFold(o, p) {
				return
			}
		}
		out = append(out, p)
	}
	for _, env := range []string{"OneDrive", "OneDriveCommercial", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "ProgramData", "ProgramFiles", "ProgramFiles(x86)", "PUBLIC", "HOME"} {
		if v := os.Getenv(env); v != "" {
			add(v)
			if env == "USERPROFILE" || env == "HOME" {
				add(filepath.Join(v, "Documents"))
				add(filepath.Join(v, "Dokumente"))
				add(filepath.Join(v, "Desktop"))
				add(filepath.Join(v, "Downloads"))
			}
		}
	}
	for _, d := range fixedDrives() {
		add(d)
	}
	return out
}

// scanForOptiDir sucht unterhalb von base (bis Tiefe depth) einen Ordner,
// dessen Name "OptiTime" enthält. Bei mehreren Treffern gewinnt der mit Datendateien.
func scanForOptiDir(base string, depth int) string {
	var hits []string
	var walk func(dir string, d int)
	walk = func(dir string, d int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") || strings.EqualFold(name, "Windows") {
				continue
			}
			full := filepath.Join(dir, name)
			if optiNameRe.MatchString(name) {
				hits = append(hits, full)
				continue
			}
			if d > 1 {
				walk(full, d-1)
			}
		}
	}
	walk(base, depth)
	if len(hits) == 0 {
		return ""
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return len(listDataFiles(hits[i])) > len(listDataFiles(hits[j]))
	})
	return hits[0]
}

var dataExt = map[string]bool{".csv": true, ".txt": true, ".json": true, ".tsv": true, ".xml": true, ".log": true, ".dat": true, ".asc": true,
	".ini": true, ".cfg": true, ".conf": true}

// fileMagic erkennt den Dateityp an den ersten Bytes.
func fileMagic(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 16)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch {
	case isSQLite(head):
		return "sqlite"
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		return "zip"
	case bytes.HasPrefix(head, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return "ole"
	case bytes.HasPrefix(head, []byte("\x00\x01\x00\x00Standard Jet")), bytes.HasPrefix(head, []byte("\x00\x01\x00\x00Standard ACE")):
		return "access"
	case bytes.HasPrefix(head, []byte("%PDF")):
		return "pdf"
	}
	return ""
}

// listDataFiles liefert alle Datendateien unterhalb eines OptiTime-Ordners (max. Tiefe 6).
func listDataFiles(root string) []string {
	files, _ := listFiles(root)
	return files
}

// listFiles liefert lesbare Datendateien und zählt die übrigen Dateitypen.
func listFiles(root string) (files []string, other map[string]int) {
	other = map[string]int{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			if rel != "." && strings.Count(rel, string(filepath.Separator)) >= 6 {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		info, err := d.Info()
		if err != nil || info.Size() == 0 {
			return nil
		}
		magic := fileMagic(p)
		if (dataExt[ext] || magic == "sqlite") && info.Size() < 200<<20 {
			files = append(files, p)
		} else {
			if ext == "" {
				ext = "(ohne Endung)"
			}
			if magic != "" {
				ext += " [" + magic + "]"
			}
			other[ext]++
		}
		return nil
	})
	sort.Strings(files)
	return files, other
}

// ---------- Zeichensatz ----------

// decodeText wandelt Dateiinhalt in UTF-8 (BOM, UTF-16, Windows-1252).
func decodeText(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return string(b[3:])
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return decodeUTF16(b[2:], false)
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return decodeUTF16(b[2:], true)
	case len(b) > 1 && b[1] == 0 && b[0] != 0:
		return decodeUTF16(b, false)
	case utf8.Valid(b):
		return string(b)
	}
	return decodeCP1252(b)
}

func decodeUTF16(b []byte, bigEndian bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return string(utf16.Decode(u))
}

var cp1252High = [32]rune{
	0x20AC, 0xFFFD, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0xFFFD, 0x017D, 0xFFFD,
	0xFFFD, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0xFFFD, 0x017E, 0x0178,
}

func decodeCP1252(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case c < 0xA0:
			sb.WriteRune(cp1252High[c-0x80])
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

// ---------- Spaltenerkennung ----------

func normHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	r := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "é", "e")
	s = r.Replace(s)
	var sb strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			sb.WriteRune(c)
		}
	}
	return sb.String()
}

var columnAliases = map[string][]string{
	"date":     {"datum", "tag", "date", "buchungsdatum", "arbeitstag"},
	"from":     {"von", "beginn", "start", "anfang", "kommen", "startzeit", "beginnzeit", "vonzeit"},
	"to":       {"bis", "ende", "end", "gehen", "endezeit", "endzeit", "biszeit"},
	"time":     {"uhrzeit", "zeit", "time", "buchungszeit"},
	"event":    {"buchung", "buchungsart", "buchungstyp", "ereignis", "typ", "art", "aktion", "stempelart", "buchungstext", "buchungskennzeichen"},
	"order":    {"auftrag", "auftragsnr", "auftragsnummer", "kostenstelle", "projekt"},
	"activity": {"taetigkeit", "aktivitaet", "taetigkeitsbezeichnung", "beschreibung"},
	"kind":     {"zeitart", "zeittyp", "kategorie"},
	"note":     {"bemerkung", "kommentar", "notiz", "hinweis", "info"},
	"person":   {"mitarbeiter", "mitarbeiterin", "person", "name", "personalnummer", "persnr", "personalnr", "mitarbeiternr", "mitarbeiternummer", "manr", "benutzer", "user", "login", "kennung", "anmeldename", "mitarbeitername", "nachname", "employee", "userid", "kartennr", "ausweisnr", "mitarbeiterid", "personid", "employeeid", "username", "benutzername", "persid"},
	"datetime": {"zeitstempel", "timestamp", "datumuhrzeit", "datumzeit", "stempelzeit", "buchungszeitpunkt", "zeitpunkt", "datetime", "erfasstam", "erfassung"},
}

type columns struct {
	idx     map[string]int
	persons []int // alle Spalten mit Personenbezug (Name, Personalnummer, Login …)
}

// columnOrder: feste Reihenfolge, damit die Erkennung deterministisch ist.
var columnOrder = []string{"date", "datetime", "from", "to", "time", "event", "order", "activity", "kind", "note", "person"}

func (c columns) has(k string) bool { _, ok := c.idx[k]; return ok }
func (c columns) get(row []string, k string) string {
	i, ok := c.idx[k]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func detectColumns(header []string) (columns, int) {
	c := columns{idx: map[string]int{}}
	score := 0
	for i, h := range header {
		n := normHeader(h)
		if n == "" {
			continue
		}
	keys:
		for _, key := range columnOrder {
			for _, a := range columnAliases[key] {
				if n != a {
					continue
				}
				if key == "person" {
					c.persons = append(c.persons, i)
					score++
					break keys
				}
				if _, taken := c.idx[key]; !taken {
					c.idx[key] = i
					score++
				}
				break keys
			}
		}
	}
	return c, score
}

// personsOf liefert alle Personenwerte einer Zeile (z. B. Personalnummer und Name).
func (c columns) personsOf(row []string) []string {
	var out []string
	for _, i := range c.persons {
		if i < len(row) {
			if v := strings.TrimSpace(row[i]); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// personDisplay: der Name, falls vorhanden, sonst die erste Kennung.
func personDisplay(parts []string) string {
	for _, p := range parts {
		if strings.IndexFunc(p, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return p
		}
	}
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// ---------- Zeit- und Datumsformate ----------

var (
	dateDE  = regexp.MustCompile(`^(\d{1,2})\.(\d{1,2})\.(\d{2,4})`)
	dateISO = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})`)
	dateUS  = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})`)
	timeRe  = regexp.MustCompile(`(\d{1,2})[:.](\d{2})(?::\d{2})?`)
)

func parseDate(s string) string {
	s = strings.TrimSpace(s)
	if m := dateISO.FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("%s-%s-%s", m[1], m[2], m[3])
	}
	if m := dateDE.FindStringSubmatch(s); m != nil {
		y := m[3]
		if len(y) == 2 {
			y = "20" + y
		}
		return fmt.Sprintf("%s-%02s-%02s", y, m[2], m[1])
	}
	if m := dateUS.FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("%s-%02s-%02s", m[3], m[2], m[1])
	}
	return ""
}

func parseTime(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "offen" || s == "open" || s == "-" || s == "--:--" {
		return ""
	}
	// Bei "dd.mm.yyyy hh:mm" die Uhrzeit hinter dem Datum nehmen.
	if i := strings.IndexAny(s, " t"); i > 0 && (dateDE.MatchString(s) || dateISO.MatchString(s)) {
		s = s[i+1:]
	}
	m := timeRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	var h, mi int
	fmt.Sscanf(m[1], "%d", &h)
	fmt.Sscanf(m[2], "%d", &mi)
	if h > 23 || mi > 59 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", h, mi)
}

// ---------- Einlesen ----------

type rawEntry struct {
	persons                                                      []string // alle Personenwerte der Zeile
	person, date, start, end, order, activity, kind, note, event string
	optiKind                                                     int // Ereignistyp aus dem OptiTime-Protokoll
}

func detectDelimiter(line string) rune {
	best, bestN := ';', strings.Count(line, ";")
	for _, d := range []rune{'\t', ',', '|'} {
		if n := strings.Count(line, string(d)); n > bestN {
			best, bestN = d, n
		}
	}
	return best
}

func readRows(text string) ([][]string, error) {
	lines := strings.SplitN(strings.TrimLeft(text, "\r\n"), "\n", 2)
	delim := detectDelimiter(lines[0])
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delim
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	var rows [][]string
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return rows, err
		}
		rows = append(rows, rec)
	}
	return rows, nil
}

// parseFile liest eine Datei und liefert Rohzeilen samt erkanntem Format.
func parseFile(path string, master *optiMaster) (entries []rawEntry, format string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "unbekannt", err
	}
	if isSQLite(b) {
		return parseSQLiteFile(path)
	}
	text := decodeText(b)
	if classifyOptiFile(path, text) == "log" {
		e, lerr := parseOptiLog(text, master)
		return e, "optitime-protokoll", lerr
	}
	if strings.EqualFold(filepath.Ext(path), ".json") || strings.HasPrefix(strings.TrimSpace(text), "{") || strings.HasPrefix(strings.TrimSpace(text), "[") {
		return parseJSON(text)
	}
	if strings.EqualFold(filepath.Ext(path), ".xml") || strings.HasPrefix(strings.TrimSpace(text), "<") {
		return parseXML(text)
	}
	rows, rerr := readRows(text)
	if len(rows) == 0 {
		if rerr != nil {
			return nil, "unbekannt", rerr
		}
		return nil, "unbekannt", errors.New("leer")
	}
	// Kopfzeile: erste der ersten 20 Zeilen mit mindestens zwei bekannten Spalten
	headerIdx, cols := -1, columns{}
	for i := 0; i < len(rows) && i < 20; i++ {
		c, score := detectColumns(rows[i])
		if score >= 2 && c.has("date") || (c.has("datetime") && score >= 2) {
			headerIdx, cols = i, c
			break
		}
	}
	if headerIdx < 0 {
		return nil, "unbekannt", errors.New("keine Kopfzeile mit Datum/von/bis oder Datum/Uhrzeit/Buchung gefunden")
	}
	switch {
	case cols.has("from") && (cols.has("to") || cols.has("time")):
		format = "intervall"
	case cols.has("event") && (cols.has("time") || cols.has("datetime") || cols.has("from")):
		format = "ereignisse"
	case cols.has("from"):
		format = "intervall"
	default:
		return nil, "unbekannt", errors.New("Spalten von/bis oder Uhrzeit/Buchung fehlen")
	}
	for _, row := range rows[headerIdx+1:] {
		e := rawEntry{
			persons: cols.personsOf(row), order: cols.get(row, "order"), activity: cols.get(row, "activity"),
			kind: cols.get(row, "kind"), note: cols.get(row, "note"), event: cols.get(row, "event"),
		}
		e.person = personDisplay(e.persons)
		if cols.has("datetime") {
			dt := cols.get(row, "datetime")
			e.date, e.start = parseDate(dt), parseTime(dt)
		}
		if d := parseDate(cols.get(row, "date")); d != "" {
			e.date = d
		}
		if format == "intervall" {
			e.start = parseTime(cols.get(row, "from"))
			if cols.has("to") {
				e.end = parseTime(cols.get(row, "to"))
			}
		} else {
			if t := parseTime(cols.get(row, "time")); t != "" {
				e.start = t
			} else if t := parseTime(cols.get(row, "from")); t != "" {
				e.start = t
			}
		}
		if e.date == "" || e.start == "" {
			continue
		}
		entries = append(entries, e)
	}
	return entries, format, nil
}

var digitsRe = regexp.MustCompile(`^-?\d+$`)

// normalizeValue macht Zahlen-Zeitstempel lesbar: Unix-Sekunden/-Millisekunden,
// .NET-Ticks (100 ns seit 0001) und OLE-Datum (Tage seit 1899-12-30).
func normalizeValue(v string) string {
	v = strings.TrimSpace(v)
	if !digitsRe.MatchString(v) {
		if f, ok := parseFloatDE(v); ok && f > 20000 && f < 80000 { // OLE Automation Date
			t := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).Add(time.Duration(f * float64(24*time.Hour)))
			return t.Format("2006-01-02 15:04")
		}
		return v
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return v
	}
	var t time.Time
	switch {
	case n > 1_000_000_000 && n < 4_000_000_000: // Unix-Sekunden
		t = time.Unix(n, 0).In(time.Local)
	case n > 1_000_000_000_000 && n < 4_000_000_000_000: // Unix-Millisekunden
		t = time.UnixMilli(n).In(time.Local)
	case n > 600_000_000_000_000_000 && n < 700_000_000_000_000_000: // .NET-Ticks
		t = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(n/10) * time.Microsecond)
	default:
		return v
	}
	return t.Format("2006-01-02 15:04")
}

func parseFloatDE(s string) (float64, bool) {
	s = strings.Replace(strings.TrimSpace(s), ",", ".", 1)
	if !regexp.MustCompile(`^\d+\.\d+$`).MatchString(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// entryFromMap baut aus Feldname->Wert (Spaltenaliase) einen Rohdatensatz.
func entryFromMap(m map[string]string) (rawEntry, bool) {
	get := func(key string) string {
		for k, v := range m {
			n := normHeader(k)
			for _, a := range columnAliases[key] {
				if n == a {
					if key == "date" || key == "datetime" || key == "from" || key == "to" || key == "time" {
						return normalizeValue(v)
					}
					return strings.TrimSpace(v)
				}
			}
		}
		return ""
	}
	e := rawEntry{order: get("order"), activity: get("activity"), kind: get("kind"), note: get("note"), event: get("event")}
	for k, v := range m {
		n := normHeader(k)
		for _, a := range columnAliases["person"] {
			if n == a {
				if sv := strings.TrimSpace(v); sv != "" {
					e.persons = append(e.persons, sv)
				}
			}
		}
	}
	sort.Strings(e.persons)
	e.person = personDisplay(e.persons)
	if dt := get("datetime"); dt != "" {
		e.date, e.start = parseDate(dt), parseTime(dt)
	}
	if d := parseDate(get("date")); d != "" {
		e.date = d
	}
	if t := parseTime(get("from")); t != "" {
		e.start = t
	} else if t := parseTime(get("time")); t != "" {
		e.start = t
	}
	e.end = parseTime(get("to"))
	return e, e.date != "" && e.start != ""
}

// xmlNode: generischer XML-Baum, damit beliebige Exportstrukturen gelesen werden können.
type xmlNode struct {
	name     string
	attrs    map[string]string
	text     string
	children []*xmlNode
}

func parseXML(text string) ([]rawEntry, string, error) {
	dec := xml.NewDecoder(strings.NewReader(text))
	dec.Strict = false
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	root := &xmlNode{name: "root", attrs: map[string]string{}}
	stack := []*xmlNode{root}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "xml", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, n)
			stack = append(stack, n)
		case xml.CharData:
			stack[len(stack)-1].text += string(t)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	var entries []rawEntry
	var walk func(n *xmlNode)
	walk = func(n *xmlNode) {
		m := map[string]string{}
		for k, v := range n.attrs {
			m[k] = v
		}
		for _, c := range n.children {
			if len(c.children) == 0 {
				m[c.name] = strings.TrimSpace(c.text)
			}
		}
		if e, ok := entryFromMap(m); ok && len(m) >= 2 {
			entries = append(entries, e)
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	if len(entries) == 0 {
		return nil, "xml", errors.New("keine Datensätze mit Datum und Uhrzeit gefunden")
	}
	return entries, "xml", nil
}

// parseSQLiteFile liest alle Tabellen einer SQLite-Datei; Tabellen, deren Spalten
// Datum und Uhrzeit hergeben, liefern Datensätze. Personen werden über eine
// Stammdatentabelle (Nr/Name) aufgelöst, damit Namen zuordenbar sind.
func parseSQLiteFile(path string) ([]rawEntry, string, error) {
	db, err := openSQLite(path)
	if err != nil {
		return nil, "sqlite", err
	}
	tables, err := db.tables()
	if err != nil {
		return nil, "sqlite", err
	}
	// Stammdaten: Tabellen mit genau einer Kennung + Name -> Nr => Name
	names := map[string]string{}
	for _, t := range tables {
		var idCol, nameCol string
		for _, c := range t.Columns {
			n := normHeader(c)
			switch {
			case nameCol == "" && (n == "name" || n == "mitarbeitername" || n == "nachname" || n == "bezeichnung"):
				nameCol = c
			case idCol == "" && (n == "nr" || n == "id" || n == "personalnummer" || n == "persnr" || n == "manr" || n == "mitarbeiternr" || n == "mitarbeiterid" || n == "personid"):
				idCol = c
			}
		}
		if idCol == "" || nameCol == "" || len(t.Columns) > 12 {
			continue
		}
		rows, err := db.rows(t, 5000)
		if err != nil {
			continue
		}
		for _, r := range rows {
			if r[idCol] != "" && r[nameCol] != "" {
				names[r[idCol]] = r[nameCol]
			}
		}
	}
	var entries []rawEntry
	var used []string
	for _, t := range tables {
		rows, err := db.rows(t, 0)
		if err != nil {
			continue
		}
		cnt := 0
		for _, r := range rows {
			e, ok := entryFromMap(r)
			if !ok {
				continue
			}
			// Kennungen (z. B. MitarbeiterID) um den Namen aus den Stammdaten ergänzen
			for _, p := range append([]string(nil), e.persons...) {
				if n, ok := names[p]; ok {
					e.persons = append(e.persons, n)
				}
			}
			e.person = personDisplay(e.persons)
			entries = append(entries, e)
			cnt++
		}
		if cnt > 0 {
			used = append(used, t.Name)
		}
	}
	if len(entries) == 0 {
		var tn []string
		for _, t := range tables {
			tn = append(tn, t.Name)
		}
		return nil, "sqlite", fmt.Errorf("keine Tabelle mit Datum/Uhrzeit-Spalten (Tabellen: %s)", strings.Join(tn, ", "))
	}
	return entries, "sqlite (" + strings.Join(used, ", ") + ")", nil
}

func parseJSON(text string) ([]rawEntry, string, error) {
	var any interface{}
	if err := json.Unmarshal([]byte(text), &any); err != nil {
		return nil, "json", err
	}
	var list []interface{}
	switch v := any.(type) {
	case []interface{}:
		list = v
	case map[string]interface{}:
		for _, k := range []string{"bookings", "buchungen", "data", "rows", "items"} {
			if l, ok := v[k].([]interface{}); ok {
				list = l
				break
			}
		}
	}
	var entries []rawEntry
	for _, it := range list {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		sm := map[string]string{}
		for k, v := range m {
			sm[k] = fmt.Sprint(v)
		}
		if e, ok := entryFromMap(sm); ok {
			entries = append(entries, e)
		}
	}
	return entries, "json", nil
}

// ---------- Zuordnung zum Benutzer ----------

func normPerson(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(s)
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}), " ")
}

func tokens(s string) []string {
	var out []string
	for _, t := range strings.Fields(normPerson(s)) {
		if len(t) >= 3 {
			out = append(out, t)
		}
	}
	return out
}

// entryMatches prüft alle Personenwerte einer Zeile (Name, Personalnummer, Login).
func entryMatches(e rawEntry, id Identity) bool {
	if matchesIdentity(e.person, id) {
		return true
	}
	for _, p := range e.persons {
		if matchesIdentity(p, id) {
			return true
		}
	}
	return false
}

// matchesIdentity prüft, ob ein Personenwert dem Benutzer entspricht.
func matchesIdentity(person string, id Identity) bool {
	p := normPerson(person)
	if p == "" {
		return false
	}
	for _, k := range id.Keys {
		nk := normPerson(k)
		if nk == "" {
			continue
		}
		if p == nk {
			return true
		}
		// Namen: alle Tokens der Kennung (z. B. "David Halko") im Wert enthalten, egal in welcher Reihenfolge
		toks := tokens(nk)
		if len(toks) >= 2 && containsAll(strings.Fields(p), toks) {
			return true
		}
	}
	return false
}

func containsAll(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// autoMatch versucht, aus den in den Dateien gefundenen Personen eindeutig den
// Benutzer zu bestimmen: genau eine Person enthält alle Tokens des Windows-
// Anzeigenamens bzw. den Nachnamen aus dem Anmeldenamen (z. B. "d.halko" -> "halko").
func autoMatch(persons []string, id Identity) string {
	var cands [][]string
	if t := tokens(id.FullName); len(t) > 0 {
		cands = append(cands, t)
	}
	if t := tokens(id.Username); len(t) > 0 {
		longest := t[0]
		for _, x := range t {
			if len(x) > len(longest) {
				longest = x
			}
		}
		cands = append(cands, []string{longest})
	}
	for _, toks := range cands {
		var hits []string
		for _, p := range persons {
			if containsAll(strings.Fields(normPerson(p)), toks) {
				hits = append(hits, p)
			}
		}
		if len(hits) == 1 {
			return hits[0]
		}
	}
	return ""
}

// inUserProfile: liegt die Datei im Profil des Benutzers (C:\Users\<Benutzer>\…)?
func inUserProfile(path, profile string) bool {
	if profile != "" {
		if rel, err := filepath.Rel(profile, path); err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

// ---------- Ereignisse zu Buchungen ----------

func classifyEvent(s string) string {
	n := normPerson(s)
	has := func(subs ...string) bool {
		for _, x := range subs {
			if strings.Contains(n, x) {
				return true
			}
		}
		return false
	}
	switch {
	case has("pause"):
		if has("ende", "zurueck", "weiter", "fortsetz", "beendet") {
			return "resume"
		}
		return "break"
	case has("kommen", "anwesend", "beginn", "start", "einstemp"), n == "k", n == "a", n == "ein":
		return "in"
	case has("gehen", "ende", "feierabend", "abwesend", "ausstemp", "schluss"), n == "g", n == "e", n == "aus":
		return "out"
	}
	return ""
}

func pairEvents(entries []rawEntry, file string) []Booking {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].date != entries[j].date {
			return entries[i].date < entries[j].date
		}
		return entries[i].start < entries[j].start
	})
	var out []Booking
	var open *Booking
	closeOpen := func(t string) {
		if open != nil {
			e := t
			open.End = &e
			out = append(out, *open)
			open = nil
		}
	}
	lastDate := ""
	for _, e := range entries {
		if e.date != lastDate {
			if open != nil { // Tag endete ohne Gehen -> offen lassen
				out = append(out, *open)
				open = nil
			}
			lastDate = e.date
		}
		switch classifyEvent(e.event) {
		case "in":
			if open != nil && open.Kind == kindBreak {
				closeOpen(e.start)
			}
			if open == nil {
				b := newBooking("work", e.date, e.start, nil, e.note, file, e.person)
				open = &b
			}
		case "break":
			closeOpen(e.start)
			b := newBooking("break", e.date, e.start, nil, e.note, file, e.person)
			open = &b
		case "resume":
			closeOpen(e.start)
			b := newBooking("work", e.date, e.start, nil, e.note, file, e.person)
			open = &b
		case "out":
			closeOpen(e.start)
		}
	}
	if open != nil {
		out = append(out, *open)
	}
	return out
}

func intervalBookings(entries []rawEntry, file string) []Booking {
	var out []Booking
	for _, e := range entries {
		typ := "work"
		if strings.Contains(strings.ToLower(e.kind), "pause") || strings.Contains(strings.ToLower(e.activity), "pause") || strings.Contains(strings.ToLower(e.event), "pause") || strings.TrimSpace(e.order) == "000007" {
			typ = "break"
		}
		var end *string
		if e.end != "" {
			v := e.end
			end = &v
		}
		b := newBooking(typ, e.date, e.start, end, e.note, file, e.person)
		if e.order != "" {
			b.Order = e.order
		}
		if e.activity != "" {
			b.Activity = e.activity
		}
		out = append(out, b)
	}
	return out
}

// ---------- Gesamtablauf ----------

// importOptiTime liest alle Datendateien unter path und liefert die Buchungen des Benutzers.
func importOptiTime(path string, id Identity) ([]Booking, SyncResult) {
	res := SyncResult{At: time.Now(), Path: path, Identity: id, Files: []FileResult{}, Persons: []string{}, Errors: []string{}}
	files, other := listFiles(path)
	res.OtherFiles = other

	// Vorlauf: Stammdaten und Konfiguration des Terminals einlesen, damit
	// Tätigkeitsnummern und Personalnummern aufgelöst werden können.
	master := loadMasterFrom(files)
	res.Master = MasterInfo{Persons: len(master.persons), Activities: len(master.activities),
		Orders: len(master.orders), PauseActivity: master.pauseKst, WorkActivity: master.workKst}
	type parsed struct {
		file    string
		format  string
		entries []rawEntry
	}
	var all []parsed
	personSet := map[string]string{}
	for _, f := range files {
		if label, ok := master.files[f]; ok {
			res.Files = append(res.Files, FileResult{Path: f, Format: label, Rows: master.rows[f]})
			continue
		}
		entries, format, err := parseFile(f, master)
		fr := FileResult{Path: f, Format: format, Rows: len(entries)}
		if err != nil {
			fr.Reason = err.Error()
			res.Files = append(res.Files, fr)
			continue
		}
		for _, e := range entries {
			if e.person != "" {
				personSet[normPerson(e.person)] = e.person
			}
		}
		all = append(all, parsed{f, format, entries})
		res.Files = append(res.Files, fr)
	}
	for _, p := range personSet {
		res.Persons = append(res.Persons, p)
	}
	sort.Strings(res.Persons)

	// Zuordnung: erst über die bestätigten Kennungen, sonst automatisch über den
	// Windows-Anzeigenamen bzw. Anmeldenamen – nur wenn eindeutig.
	matched := ""
	confirmed := Identity{Keys: id.ConfirmedKeys}
	for _, p := range all {
		for _, e := range p.entries {
			if e.person != "" && entryMatches(e, id) {
				matched = e.person
				id.AutoMatched = !entryMatches(e, confirmed)
				break
			}
		}
		if matched != "" {
			break
		}
	}
	if matched == "" {
		if m := autoMatch(res.Persons, id); m != "" {
			matched = m
			id.Keys = append(id.Keys, m)
			id.AutoMatched = true
		}
	}
	id.MatchedPerson = matched
	res.Identity = id

	var bookings []Booking
	fi := 0
	for _, p := range all {
		for fi < len(res.Files) && res.Files[fi].Path != p.file {
			fi++
		}
		fr := &res.Files[fi]
		hasPerson := false
		for _, e := range p.entries {
			if e.person != "" {
				hasPerson = true
				break
			}
		}
		var mine []rawEntry
		if hasPerson {
			for _, e := range p.entries {
				if entryMatches(e, id) {
					mine = append(mine, e)
				} else {
					fr.Skipped++
				}
			}
		} else {
			// Ohne Personenspalte: Datei nur übernehmen, wenn Dateiname/Ordner den Benutzer nennt
			// oder die Datei im Benutzerprofil liegt.
			rel, _ := filepath.Rel(path, p.file)
			nameMatch := false
			for _, k := range id.Keys {
				for _, t := range tokens(k) {
					if strings.Contains(normPerson(rel), t) {
						nameMatch = true
					}
				}
			}
			if nameMatch || inUserProfile(p.file, id.ProfileDir) {
				mine = p.entries
			} else {
				fr.Reason = "keine Personenspalte und kein Benutzerbezug im Dateinamen – nicht zugeordnet"
				res.Unassigned++
				continue
			}
		}
		if p.format == "optitime-protokoll" {
			bs := pairOptiEvents(mine, p.file)
			fr.Taken = len(bs)
			bookings = append(bookings, bs...)
			continue
		}
		// Je Datensatz: Stempelereignis (Buchungsart, keine Endzeit) wird gepaart,
		// Intervall (von/bis) direkt übernommen – auch gemischt in einer Datei.
		var events, intervals []rawEntry
		for _, e := range mine {
			if e.end == "" && classifyEvent(e.event) != "" {
				events = append(events, e)
			} else {
				intervals = append(intervals, e)
			}
		}
		bs := intervalBookings(intervals, p.file)
		bs = append(bs, pairEvents(events, p.file)...)
		fr.Taken = len(bs)
		bookings = append(bookings, bs...)
	}
	res.Imported = len(bookings)
	return bookings, res
}
