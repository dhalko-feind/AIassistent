package main

// optilog.go – Dateien des OptiTime-Terminals lesen:
//
//	logger.txt     Protokoll; enthält die tatsächlich gestempelten Buchungen
//	Offliste*.txt  Stammdaten (Mitarbeiter, Tätigkeiten, Aufträge), µ-getrennt
//	config.ini     Einstellungen des Terminals (u. a. Tätigkeit für Pause)
//
// Aufbau einer Buchung im Protokoll:
//
//	11.09.2026 13:19:12 2/10[NB-EFEIND-0021] - StarteBuchungsvorgang
//	11.09.2026 13:19:12 2/10[NB-EFEIND-0021] - VerarbeiteBuchung: PNR=1626 KST=000007 …
//	11.09.2026 13:19:12 2/10[NB-EFEIND-0021] - Erzeuge_Buchung_Online Meldung: Halko, David hat umgestempelt.
//
// Maßgeblich ist der Zeitstempel der Protokollzeile; BEG_STD/BEG_MIN enthalten
// beide die Stunde und sind als Uhrzeit unbrauchbar.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	optiWork  = 0 // Arbeitszeit (Kommen, Auftrag, Tätigkeit)
	optiBreak = 1 // Pause
	optiEnd   = 2 // Arbeitsende / Gehen – beendet nur die laufende Buchung
)

// optiMaster sind die Stammdaten des Terminals.
type optiMaster struct {
	persons    map[string]string // Personalnummer -> "Nachname, Vorname"
	activities map[string]string // Tätigkeit (KST) -> Bezeichnung
	orders     map[string]string // Auftragsnummer -> Bezeichnung
	pauseKst   string            // KSTPAUSE aus config.ini
	workKst    string            // DEFAULTKST aus config.ini
	files      map[string]string // Pfad -> erkannte Art
	rows       map[string]int    // Pfad -> Anzahl übernommener Einträge
}

func newOptiMaster() *optiMaster {
	return &optiMaster{
		persons: map[string]string{}, activities: map[string]string{},
		orders: map[string]string{}, files: map[string]string{}, rows: map[string]int{},
	}
}

// loadMasterFrom liest aus einer Dateiliste alle Stammdaten- und
// Konfigurationsdateien des Terminals ein.
func loadMasterFrom(files []string) *optiMaster {
	m := newOptiMaster()
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil || isSQLite(b) {
			continue
		}
		text := decodeText(b)
		kind := classifyOptiFile(f, text)
		if kind == "" || kind == "log" {
			continue
		}
		if label, n := m.loadMaster(f, text, kind); label != "" {
			m.files[f], m.rows[f] = label, n
		}
	}
	return m
}

// Standardnummern des OptiTime-Terminals; sie gelten, solange weder
// config.ini noch Tätigkeitsliste etwas anderes sagen.
const (
	kstPauseDefault = "000007" // *Pause*
	kstWorkDefault  = "000999" // Kommen
	kstEndDefault   = "000000" // Arbeitsende vom System
)

// activityKind ordnet eine Tätigkeitsnummer einem Buchungstyp zu.
func (m *optiMaster) activityKind(kst string) int {
	kst = strings.TrimSpace(kst)
	bez := strings.ToLower(m.activities[kst])
	switch {
	case m.pauseKst != "" && kst == m.pauseKst:
		return optiBreak
	case bez != "":
		// Bezeichnung aus der Tätigkeitsliste hat Vorrang vor den Standardnummern
		switch {
		case strings.Contains(bez, "pause"):
			return optiBreak
		case strings.Contains(bez, "arbeitsende"), strings.Contains(bez, "feierabend"):
			return optiEnd
		}
		return optiWork
	case kst == kstPauseDefault:
		return optiBreak
	case kst == kstEndDefault:
		return optiEnd
	}
	return optiWork
}

func (m *optiMaster) activityName(kst string) string {
	if b := m.activities[strings.TrimSpace(kst)]; b != "" {
		return b
	}
	return ""
}

// ---------- Erkennung ----------

var (
	logLineRe = regexp.MustCompile(`^(\d{2})\.(\d{2})\.(\d{4}) (\d{2}):(\d{2}):(\d{2}) +\d+/\d+\[([^\]]*)\] *- *(.*)$`)
	buchungRe = regexp.MustCompile(`VerarbeiteBuchung: *PNR=(\S+) +KST=(\S+)`)
	meldungRe = regexp.MustCompile(`Erzeuge_Buchung_Online +Meldung: *(.+?) +(hat|ist|wurde) +(.*?)\.?$`)
	iniLineRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_()]* *=`)
)

// classifyOptiFile erkennt Protokoll, Stammdatenliste und Konfiguration.
func classifyOptiFile(path, text string) string {
	base := strings.ToLower(filepath.Base(path))
	head := headLines(text, 20)
	switch {
	case isOffliste(head):
		switch {
		case strings.Contains(base, "taet"), strings.Contains(base, "kst"):
			return "offliste-taet"
		case strings.Contains(base, "auf"):
			return "offliste-auf"
		case strings.Contains(base, "ma"):
			return "offliste-ma"
		}
		return "offliste"
	case isOptiLog(head):
		return "log"
	case isIni(head) && (strings.Contains(base, "config") || strings.Contains(text, "KSTPAUSE=")):
		return "config"
	}
	return ""
}

func headLines(text string, n int) []string {
	var out []string
	for _, l := range strings.SplitN(text, "\n", n+1) {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
		if len(out) >= n {
			break
		}
	}
	return out
}

func isOffliste(head []string) bool {
	ok := 0
	for _, l := range head {
		if strings.HasPrefix(l, "µ") && strings.Count(l, "µ") >= 4 {
			ok++
		}
	}
	return ok > 0 && ok >= len(head)/2
}

func isOptiLog(head []string) bool {
	ok := 0
	for _, l := range head {
		if logLineRe.MatchString(l) {
			ok++
		}
	}
	return ok > 0 && ok >= len(head)/2
}

func isIni(head []string) bool {
	ok := 0
	for _, l := range head {
		if iniLineRe.MatchString(l) {
			ok++
		}
	}
	return ok > 0 && ok >= len(head)/2
}

// ---------- Stammdaten ----------

// parseOffliste zerlegt eine µ-getrennte Liste. Feld 0 ist immer leer.
func parseOffliste(text string) [][]string {
	var rows [][]string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if !strings.HasPrefix(l, "µ") {
			continue
		}
		f := strings.Split(l, "µ")
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		rows = append(rows, f)
	}
	return rows
}

func field(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

// loadMaster liest eine Stammdaten- oder Konfigurationsdatei ein und liefert
// die Art sowie die Anzahl übernommener Einträge.
func (m *optiMaster) loadMaster(path, text, kind string) (string, int) {
	n := 0
	switch kind {
	case "offliste-ma":
		for _, f := range parseOffliste(text) {
			pnr, nach, vor := field(f, 1), field(f, 2), field(f, 3)
			if pnr == "" || nach == "" {
				continue
			}
			name := nach
			if vor != "" {
				name = nach + ", " + vor
			}
			m.persons[pnr] = name
			n++
		}
		return "stammdaten (Mitarbeiter)", n
	case "offliste-taet":
		for _, f := range parseOffliste(text) {
			if kst, bez := field(f, 1), field(f, 2); kst != "" && bez != "" {
				m.activities[kst] = bez
				n++
			}
		}
		return "stammdaten (Tätigkeiten)", n
	case "offliste-auf":
		for _, f := range parseOffliste(text) {
			if nr, bez := field(f, 1), field(f, 2); nr != "" && bez != "" {
				m.orders[nr] = bez
				n++
			}
		}
		return "stammdaten (Aufträge)", n
	case "offliste":
		return "stammdaten (unbekannte Liste)", len(parseOffliste(text))
	case "config":
		for _, l := range strings.Split(text, "\n") {
			l = strings.TrimRight(l, "\r")
			k, v, ok := strings.Cut(l, "=")
			if !ok {
				continue
			}
			switch strings.ToUpper(strings.TrimSpace(k)) {
			case "KSTPAUSE":
				m.pauseKst = strings.TrimSpace(v)
			case "DEFAULTKST":
				m.workKst = strings.TrimSpace(v)
			}
			n++
		}
		return "konfiguration", n
	}
	return "", 0
}

// redactOffliste entfernt die Kennwortspalte (Feld 6) der Mitarbeiterliste.
func redactOffliste(line, kind string) string {
	line = strings.TrimRight(line, "\r")
	if kind != "offliste-ma" || !strings.HasPrefix(line, "µ") {
		return line
	}
	f := strings.Split(line, "µ")
	for _, i := range []int{5, 6} {
		if i < len(f) && f[i] != "" {
			f[i] = "<entfernt>"
		}
	}
	return strings.Join(f, "µ")
}

// ---------- Protokoll ----------

type optiEvent struct {
	date, time string
	pnr, kst   string
	person     string
	ok         bool
	note       string
}

// parseOptiLog liest die Buchungen aus dem Terminal-Protokoll.
func parseOptiLog(text string, m *optiMaster) ([]rawEntry, error) {
	if m == nil {
		m = newOptiMaster()
	}
	var events []optiEvent
	var pending *optiEvent
	flush := func() {
		if pending != nil {
			if pending.ok {
				events = append(events, *pending)
			}
			pending = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		mm := logLineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if mm == nil {
			continue
		}
		date := mm[3] + "-" + mm[2] + "-" + mm[1]
		hhmm := mm[4] + ":" + mm[5]
		msg := mm[8]
		if b := buchungRe.FindStringSubmatch(msg); b != nil {
			flush()
			pending = &optiEvent{date: date, time: hhmm, pnr: b[1], kst: b[2], ok: true}
			continue
		}
		if pending == nil {
			continue
		}
		if md := meldungRe.FindStringSubmatch(msg); md != nil {
			pending.person = strings.TrimSpace(md[1])
			// "hat angestempelt/umgestempelt/ausgestempelt" = gebucht,
			// alles andere (z. B. "ist noch gar nicht da") = abgewiesen.
			if md[2] != "hat" || !strings.Contains(md[3], "gestempelt") {
				pending.ok = false
				pending.note = strings.TrimSpace(md[2] + " " + md[3])
			}
			continue
		}
		if strings.HasPrefix(msg, "StarteBuchungsvorgang") || strings.Contains(msg, "Gehe zum Grundbild") {
			flush()
		}
	}
	flush()
	if len(events) == 0 {
		return nil, fmt.Errorf("keine Buchungen im Protokoll gefunden")
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].date != events[j].date {
			return events[i].date < events[j].date
		}
		return events[i].time < events[j].time
	})
	out := make([]rawEntry, 0, len(events))
	for _, e := range events {
		r := rawEntry{date: e.date, start: e.time, order: e.kst, optiKind: m.activityKind(e.kst), note: e.note}
		r.activity = m.activityName(e.kst)
		if r.activity == "" {
			r.activity = e.kst
		}
		r.event = r.activity
		if e.pnr != "" {
			r.persons = append(r.persons, e.pnr)
		}
		name := e.person
		if name == "" {
			name = m.persons[e.pnr]
		}
		if name != "" {
			r.persons = append(r.persons, name)
		}
		r.person = personDisplay(r.persons)
		out = append(out, r)
	}
	return out, nil
}

// pairOptiEvents bildet aus den Stempelereignissen Buchungen. Jede Buchung
// beendet die vorherige (Umstempeln); ein Arbeitsende beendet ohne neue
// Buchung. Fehlt das Arbeitsende, bleibt der Tag offen.
func pairOptiEvents(entries []rawEntry, file string) []Booking {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].date != entries[j].date {
			return entries[i].date < entries[j].date
		}
		return entries[i].start < entries[j].start
	})
	var out []Booking
	var open *Booking
	lastDate := ""
	for _, e := range entries {
		if e.date != lastDate {
			if open != nil { // Tag endete ohne Arbeitsende – Buchung bleibt offen
				out = append(out, *open)
				open = nil
			}
			lastDate = e.date
		}
		if open != nil {
			end := e.start
			open.End = &end
			out = append(out, *open)
			open = nil
		}
		if e.optiKind == optiEnd {
			continue
		}
		typ := "work"
		if e.optiKind == optiBreak {
			typ = "break"
		}
		b := newBooking(typ, e.date, e.start, nil, e.note, file, e.person)
		if e.order != "" {
			b.Order = e.order
		}
		if e.activity != "" {
			b.Activity = e.activity
		}
		open = &b
	}
	if open != nil {
		out = append(out, *open)
	}
	return out
}
