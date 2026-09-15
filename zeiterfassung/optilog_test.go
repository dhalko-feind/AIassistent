package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Nachbau des Formats des OptiTime-Terminals – keine echten Personendaten.
const testLog = `20.08.2026 13:01:05 1/10[NB-TEST-01] - Programm gestartet
20.08.2026 13:01:05 2/10[NB-TEST-01] - LadeOfflineListen
21.08.2026 06:44:04 2/10[NB-TEST-01] - StarteBuchungsvorgang
21.08.2026 06:44:04 2/10[NB-TEST-01] - 1626 -1 000999
21.08.2026 06:44:04 2/10[NB-TEST-01] - VerarbeiteBuchung: PNR=1626 KST=000999 BEG_STD=6 BEG_MIN=6
21.08.2026 06:44:04 2/10[NB-TEST-01] - Erzeuge_Buchung_Online Meldung: Muster, Max hat angestempelt.
21.08.2026 06:44:08 2/10[NB-TEST-01] - StarteBuchungsvorgang beendet
21.08.2026 09:21:49 2/10[NB-TEST-01] - VerarbeiteBuchung: PNR=1626 KST=000007 BEG_STD=9 BEG_MIN=9
21.08.2026 09:21:49 2/10[NB-TEST-01] - Erzeuge_Buchung_Online Meldung: Muster, Max hat umgestempelt.
21.08.2026 09:30:21 2/10[NB-TEST-01] - VerarbeiteBuchung: PNR=1626 KST=000999 BEG_STD=9 BEG_MIN=9
21.08.2026 09:30:21 2/10[NB-TEST-01] - Erzeuge_Buchung_Online Meldung: Muster, Max hat umgestempelt.
21.08.2026 15:33:04 2/10[NB-TEST-01] - VerarbeiteBuchung: PNR=1626 KST=000000 BEG_STD=15 BEG_MIN=15
21.08.2026 15:33:04 2/10[NB-TEST-01] - Erzeuge_Buchung_Online Meldung: Muster, Max hat ausgestempelt.
21.08.2026 15:40:00 2/10[NB-TEST-01] - VerarbeiteBuchung: PNR=1626 KST=000000 BEG_STD=15 BEG_MIN=15
21.08.2026 15:40:00 2/10[NB-TEST-01] - Erzeuge_Buchung_Online Meldung: Muster, Max ist noch gar nicht da.
24.08.2026 07:00:00 2/10[NB-TEST-01] - VerarbeiteBuchung: PNR=1626 KST=000999 BEG_STD=7 BEG_MIN=7
24.08.2026 07:00:00 2/10[NB-TEST-01] - Erzeuge_Buchung_Online Meldung: Muster, Max hat angestempelt.
24.08.2026 08:00:00 2/10[NB-TEST-01] - VerarbeiteBuchung: PNR=2001 KST=000999 BEG_STD=8 BEG_MIN=8
24.08.2026 08:00:00 2/10[NB-TEST-01] - Erzeuge_Buchung_Online Meldung: Fremd, Erika hat angestempelt.
`

const testOfflisteMa = "µ1626µMusterµMaxµ0µµGEHEIMPINµ0µ0µ0µ1000µµµ\r\n" +
	"µ2001µFremdµErikaµ0µµµ0µ0µ0µ1000µµµ\r\n"

const testOfflisteTaet = "µ000000µArbeitsende vom Systemµ1µµµµµµµµµµµ\r\n" +
	"µ000004µSonderurlaubµ0µµµµµµµµµµµ\r\n" +
	"µ000007µ*Pause*µ1µµµµµµµµµµµ\r\n" +
	"µ000999µKommenµ1µµµµµµµµµµµ\r\n"

const testOfflisteAuf = "µ131217µL131217-AµL131217-Aµ µµµµµµµµµµµµµµµµ1µ1\r\n" +
	"µ261787µLS261787-01-AµLS261787-01-Aµ µµµµµµµµµµµµµµµµ1µ1\r\n"

const testConfigIni = "LOGGERSTUFE=10\r\nDEFAULTKST=000999\r\nKSTPAUSE=000007\r\nSMTPPW=streng-geheim\r\nSUPERPIN=\r\nANMELDWINDOWS=1\r\n"

func optiFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "logger.txt", []byte(testLog))
	write(t, dir, "OfflisteMa.txt", []byte(testOfflisteMa))
	write(t, dir, "OfflisteTaet.txt", []byte(testOfflisteTaet))
	write(t, dir, "OfflisteAuf.txt", []byte(testOfflisteAuf))
	write(t, dir, "config.ini", []byte(testConfigIni))
	return dir
}

func TestClassifyOptiFile(t *testing.T) {
	cases := map[string]string{
		"logger.txt":       "log",
		"OfflisteMa.txt":   "offliste-ma",
		"OfflisteTaet.txt": "offliste-taet",
		"OfflisteAuf.txt":  "offliste-auf",
		"config.ini":       "config",
	}
	texts := map[string]string{
		"logger.txt": testLog, "OfflisteMa.txt": testOfflisteMa, "OfflisteTaet.txt": testOfflisteTaet,
		"OfflisteAuf.txt": testOfflisteAuf, "config.ini": testConfigIni,
	}
	for name, want := range cases {
		if got := classifyOptiFile(name, texts[name]); got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
	if got := classifyOptiFile("beliebig.csv", "Datum;von;bis\n17.08.2026;08:00;15:30\n"); got != "" {
		t.Errorf("CSV darf nicht als OptiTime-Datei gelten: %q", got)
	}
}

func TestMasterUndTaetigkeitstyp(t *testing.T) {
	dir := optiFolder(t)
	m := loadMasterFrom(listDataFiles(dir))
	if m.persons["1626"] != "Muster, Max" || m.persons["2001"] != "Fremd, Erika" {
		t.Fatalf("Mitarbeiter: %v", m.persons)
	}
	if m.activities["000007"] != "*Pause*" || m.activities["000999"] != "Kommen" {
		t.Fatalf("Tätigkeiten: %v", m.activities)
	}
	if m.orders["261787"] != "LS261787-01-A" || len(m.orders) != 2 {
		t.Fatalf("Aufträge: %v", m.orders)
	}
	if m.pauseKst != "000007" || m.workKst != "000999" {
		t.Fatalf("config.ini: pause=%q work=%q", m.pauseKst, m.workKst)
	}
	if m.activityKind("000999") != optiWork || m.activityKind("000007") != optiBreak || m.activityKind("000000") != optiEnd {
		t.Fatal("Zuordnung der Tätigkeitsnummern falsch")
	}
	// Auftragsnummer als Tätigkeit gilt als Arbeitszeit
	if m.activityKind("261787") != optiWork {
		t.Fatal("unbekannte Tätigkeit muss Arbeitszeit sein")
	}
}

func TestParseOptiLog(t *testing.T) {
	dir := optiFolder(t)
	m := loadMasterFrom(listDataFiles(dir))
	entries, err := parseOptiLog(testLog, m)
	if err != nil {
		t.Fatal(err)
	}
	// 6 gültige Ereignisse; die abgewiesene Buchung (15:40) zählt nicht
	if len(entries) != 6 {
		t.Fatalf("%d Ereignisse: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.date != "2026-08-21" || e.start != "06:44" || e.optiKind != optiWork || e.person != "Muster, Max" || e.order != "000999" || e.activity != "Kommen" {
		t.Fatalf("erstes Ereignis: %+v", e)
	}
	if entries[1].optiKind != optiBreak || entries[3].optiKind != optiEnd {
		t.Fatalf("Typen: %+v", entries)
	}
	for _, e := range entries {
		if e.start == "15:40" {
			t.Fatal("abgewiesene Buchung wurde übernommen")
		}
	}
	// Personalnummer und Name stehen beide zur Zuordnung bereit
	if len(entries[0].persons) != 2 || entries[0].persons[0] != "1626" {
		t.Fatalf("Kennungen: %v", entries[0].persons)
	}
}

func TestPairOptiEvents(t *testing.T) {
	m := newOptiMaster()
	m.activities = map[string]string{"000000": "Arbeitsende vom System", "000007": "*Pause*", "000999": "Kommen"}
	entries, err := parseOptiLog(testLog, m)
	if err != nil {
		t.Fatal(err)
	}
	var mine []rawEntry
	for _, e := range entries {
		if e.persons[0] == "1626" {
			mine = append(mine, e)
		}
	}
	bs := pairOptiEvents(mine, "logger.txt")
	if len(bs) != 4 {
		t.Fatalf("%d Buchungen: %+v", len(bs), bs)
	}
	if bs[0].Start != "06:44" || *bs[0].End != "09:21" || bs[0].Kind != kindWork {
		t.Fatalf("Buchung 1: %+v", bs[0])
	}
	if bs[1].Kind != kindBreak || *bs[1].End != "09:30" {
		t.Fatalf("Buchung 2: %+v", bs[1])
	}
	// Arbeitsende beendet die laufende Buchung, ohne eine neue zu öffnen
	if *bs[2].End != "15:33" {
		t.Fatalf("Buchung 3: %+v", bs[2])
	}
	// Tag ohne Arbeitsende bleibt offen
	if bs[3].Date != "2026-08-24" || bs[3].End != nil {
		t.Fatalf("Buchung 4: %+v", bs[3])
	}
	d := computeDayFromBookings(t, bs, "2026-08-21")
	if d.workMin != 157+363 || d.breakMin != 9 {
		t.Fatalf("Tagesauswertung: work=%d break=%d", d.workMin, d.breakMin)
	}
}

// computeDayFromBookings summiert Arbeits- und Pausenzeit eines Tages.
func computeDayFromBookings(t *testing.T, bs []Booking, date string) struct{ workMin, breakMin int } {
	t.Helper()
	var out struct{ workMin, breakMin int }
	for _, b := range bs {
		if b.Date != date || b.End == nil {
			continue
		}
		min := minutesBetween(b.Start, *b.End)
		if b.Kind == kindBreak {
			out.breakMin += min
		} else {
			out.workMin += min
		}
	}
	return out
}

func minutesBetween(from, to string) int {
	var fh, fm, th, tm int
	fmt.Sscanf(from, "%d:%d", &fh, &fm)
	fmt.Sscanf(to, "%d:%d", &th, &tm)
	return (th*60 + tm) - (fh*60 + fm)
}

func TestImportOptiTimeOrdner(t *testing.T) {
	dir := optiFolder(t)
	id := Identity{Username: "m.muster", FullName: "Max Muster", Keys: []string{"m.muster", "Max Muster"}}
	bookings, res := importOptiTime(dir, id)
	if res.Identity.MatchedPerson != "Muster, Max" || !res.Identity.AutoMatched {
		t.Fatalf("Zuordnung: %+v", res.Identity)
	}
	if len(bookings) != 4 {
		t.Fatalf("%d Buchungen", len(bookings))
	}
	for _, b := range bookings {
		if b.Person != "Muster, Max" {
			t.Fatalf("fremde Buchung: %+v", b)
		}
	}
	var log, stamm int
	for _, f := range res.Files {
		switch {
		case f.Format == "optitime-protokoll":
			log++
			if f.Taken != 4 || f.Skipped != 1 {
				t.Fatalf("Protokoll: %+v", f)
			}
		case strings.HasPrefix(f.Format, "stammdaten"), f.Format == "konfiguration":
			stamm++
			if f.Rows == 0 {
				t.Fatalf("Stammdaten ohne Einträge: %+v", f)
			}
		default:
			t.Fatalf("unerwartetes Format: %+v", f)
		}
	}
	if log != 1 || stamm != 4 {
		t.Fatalf("Dateien: log=%d stammdaten=%d", log, stamm)
	}
	if res.Master.Persons != 2 || res.Master.Activities != 4 || res.Master.Orders != 2 {
		t.Fatalf("Stammdaten-Kennzahlen: %+v", res.Master)
	}
}

func TestRedaction(t *testing.T) {
	if got := redactLine("SMTPPW=streng-geheim"); got != "SMTPPW=<entfernt>" {
		t.Fatalf("config: %q", got)
	}
	if got := redactLine("SUPERPIN="); got != "SUPERPIN=" {
		t.Fatalf("leerer Wert darf bleiben: %q", got)
	}
	if got := redactLine("LOGGERSTUFE=10"); got != "LOGGERSTUFE=10" {
		t.Fatalf("harmloser Schlüssel: %q", got)
	}
	line := strings.Split(testOfflisteMa, "\r\n")[0]
	got := redactOffliste(line, "offliste-ma")
	if strings.Contains(got, "GEHEIMPIN") || !strings.Contains(got, "<entfernt>") {
		t.Fatalf("Mitarbeiterzeile: %q", got)
	}
	if !strings.Contains(got, "Muster") {
		t.Fatalf("Name muss erhalten bleiben: %q", got)
	}
	if got := redactOffliste(strings.Split(testOfflisteTaet, "\r\n")[0], "offliste-taet"); strings.Contains(got, "<entfernt>") {
		t.Fatalf("Tätigkeitsliste darf nicht maskiert werden: %q", got)
	}
}

func TestLogOhneStammdaten(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, filepath.Join("OptiTime", "logger.txt"), []byte(testLog))
	id := Identity{Username: "m.muster", FullName: "Max Muster", Keys: []string{"m.muster", "Max Muster"}}
	bookings, res := importOptiTime(filepath.Join(dir, "OptiTime"), id)
	// Ohne Tätigkeitsliste greifen die eingebauten Nummern (000007 Pause, 000000 Ende)
	if len(bookings) != 4 || res.Identity.MatchedPerson != "Muster, Max" {
		t.Fatalf("%d Buchungen, Zuordnung %q", len(bookings), res.Identity.MatchedPerson)
	}
	if bookings[1].Kind != kindBreak {
		t.Fatalf("Pause nicht erkannt: %+v", bookings[1])
	}
}
