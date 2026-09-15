package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func ident() Identity {
	return Identity{Username: "d.halko", FullName: "David Halko", Keys: []string{"d.halko", "David Halko"}}
}

func TestDecodeText(t *testing.T) {
	// Windows-1252: "Tätigkeit" mit 0xE4
	if got := decodeText([]byte{'T', 0xE4, 't'}); got != "Tät" {
		t.Fatalf("cp1252: %q", got)
	}
	if got := decodeText([]byte{0xEF, 0xBB, 0xBF, 'a'}); got != "a" {
		t.Fatalf("bom: %q", got)
	}
	if got := decodeText([]byte{0xFF, 0xFE, 'A', 0, 0xE4, 0}); got != "Aä" {
		t.Fatalf("utf16: %q", got)
	}
}

func TestParseDateTime(t *testing.T) {
	cases := map[string]string{"17.08.2026": "2026-08-17", "2026-08-17": "2026-08-17", "7.9.26": "2026-09-07", "17.08.2026 06:54": "2026-08-17"}
	for in, want := range cases {
		if got := parseDate(in); got != want {
			t.Errorf("parseDate(%q)=%q want %q", in, got, want)
		}
	}
	tcases := map[string]string{"6:54": "06:54", "06:54:30": "06:54", "offen": "", "17.08.2026 06:54": "06:54", "25:00": ""}
	for in, want := range tcases {
		if got := parseTime(in); got != want {
			t.Errorf("parseTime(%q)=%q want %q", in, got, want)
		}
	}
}

func TestImportIntervalWithPersonColumn(t *testing.T) {
	dir := t.TempDir()
	csv := "Personalnummer;Mitarbeiter;Datum;von;bis;Auftrag;T\xe4tigkeit\r\n" +
		"1042;Halko, David;07.09.2026;06:54;;000999;Kommen\r\n" +
		"1042;Halko, David;04.09.2026;15:00;15:07;000007;*Pause*\r\n" +
		"1042;Halko, David;04.09.2026;07:03;15:00;000999;Kommen\r\n" +
		"2001;Muster, Erika;04.09.2026;08:00;16:00;000999;Kommen\r\n"
	write(t, dir, "OptiTime/Export/buchungen_2026-09.csv", []byte(csv))
	bookings, res := importOptiTime(filepath.Join(dir, "OptiTime"), ident())
	if len(bookings) != 3 {
		t.Fatalf("erwartet 3 eigene Buchungen, got %d (%+v)", len(bookings), res)
	}
	if res.Identity.MatchedPerson != "Halko, David" || !res.Identity.AutoMatched {
		t.Fatalf("automatische Zuordnung fehlt: %+v", res.Identity)
	}
	if len(res.Persons) != 2 || res.Files[0].Skipped != 1 {
		t.Fatalf("Personen/Skipped: %+v", res)
	}
	for _, b := range bookings {
		if b.Person != "Halko, David" || b.Source != "optitime" {
			t.Fatalf("Buchung falsch zugeordnet: %+v", b)
		}
	}
	// Reihenfolge und Typen
	var breaks, open int
	for _, b := range bookings {
		if b.Kind == kindBreak {
			breaks++
		}
		if b.End == nil {
			open++
		}
	}
	if breaks != 1 || open != 1 {
		t.Fatalf("breaks=%d open=%d", breaks, open)
	}
}

func TestImportEventsFormat(t *testing.T) {
	dir := t.TempDir()
	csv := "Datum\tUhrzeit\tBuchung\tName\n" +
		"18.08.2026\t08:01\tKommen\tDavid Halko\n" +
		"18.08.2026\t11:16\tPause Beginn\tDavid Halko\n" +
		"18.08.2026\t11:23\tPause Ende\tDavid Halko\n" +
		"18.08.2026\t15:30\tGehen\tDavid Halko\n" +
		"18.08.2026\t08:00\tKommen\tErika Muster\n"
	write(t, dir, "terminal.txt", []byte(csv))
	bookings, res := importOptiTime(dir, ident())
	if res.Files[0].Format != "ereignisse" {
		t.Fatalf("format: %+v", res.Files[0])
	}
	if len(bookings) != 3 {
		t.Fatalf("erwartet 3 Buchungen (Arbeit, Pause, Arbeit), got %d", len(bookings))
	}
	if bookings[0].Start != "08:01" || *bookings[0].End != "11:16" || bookings[1].Kind != kindBreak || *bookings[2].End != "15:30" {
		t.Fatalf("Paarung falsch: %+v", bookings)
	}
}

func TestFileWithoutPersonColumn(t *testing.T) {
	dir := t.TempDir()
	csv := "Datum;von;bis;Auftrag;Tätigkeit\n17.08.2026;08:00;15:30;000999;Kommen\n"
	write(t, dir, "buchungen_d.halko.csv", []byte(csv)) // Dateiname nennt den Benutzer -> übernehmen
	write(t, dir, "alle.csv", []byte(csv))              // kein Bezug -> nicht zuordnen
	write(t, dir, "daten.db", []byte("binär"))          // unbekannter Typ -> nur zählen
	bookings, res := importOptiTime(dir, ident())
	if len(bookings) != 1 || res.Unassigned != 1 || res.OtherFiles[".db"] != 1 {
		t.Logf("other=%v", res.OtherFiles)
		t.Fatalf("bookings=%d unassigned=%d other=%v files=%+v", len(bookings), res.Unassigned, res.OtherFiles, res.Files)
	}
	// Liegt der Ordner im Profil des Benutzers, gelten Dateien ohne Personenspalte als seine
	id := ident()
	id.ProfileDir = dir
	bookings2, res2 := importOptiTime(dir, id)
	if len(bookings2) != 2 || res2.Unassigned != 0 {
		t.Fatalf("profil: bookings=%d unassigned=%d", len(bookings2), res2.Unassigned)
	}
}

func TestXMLImport(t *testing.T) {
	dir := t.TempDir()
	x := `<?xml version="1.0" encoding="UTF-8"?><Export><Buchung Mitarbeiter="Halko, David"><Datum>17.08.2026</Datum><Von>08:00</Von><Bis>15:30</Bis></Buchung>` +
		`<Buchung Mitarbeiter="Muster, Erika"><Datum>17.08.2026</Datum><Von>08:00</Von><Bis>15:30</Bis></Buchung>` +
		`<Stempel><Zeitstempel>18.08.2026 07:00:12</Zeitstempel><Buchungsart>Kommen</Buchungsart><Benutzer>d.halko</Benutzer></Stempel>` +
		`<Stempel><Zeitstempel>18.08.2026 15:30:00</Zeitstempel><Buchungsart>Gehen</Buchungsart><Benutzer>d.halko</Benutzer></Stempel></Export>`
	write(t, dir, "export.xml", []byte(x))
	bookings, res := importOptiTime(dir, ident())
	if res.Files[0].Format != "xml" || res.Files[0].Rows != 4 {
		t.Fatalf("xml: %+v", res.Files[0])
	}
	if len(bookings) != 2 {
		t.Fatalf("erwartet 2 eigene Buchungen (Intervall + gepaarte Stempel), got %d: %+v", len(bookings), bookings)
	}
}

func TestProfileDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("USERPROFILE", filepath.Join(dir, "Users", "admin"))
	t.Setenv("HOME", "")
	t.Setenv("SystemDrive", "")
	write(t, dir, filepath.Join("Users", "d.halko", "x.txt"), []byte("x"))
	if got := profileDir("d.halko"); got != filepath.Join(dir, "Users", "d.halko") {
		t.Fatalf("profileDir=%q", got)
	}
	if got := profileDir("admin"); got != filepath.Join(dir, "Users", "admin") {
		t.Fatalf("profileDir(admin)=%q", got)
	}
	id := currentIdentity("FEIND\\d.halko", "parameter")
	if id.Username != "d.halko" || id.Domain != "FEIND" || id.Source != "parameter" || id.ProfileDir == "" {
		t.Fatalf("identity: %+v", id)
	}
}

func TestPersonalnummerMatchesWithoutAuto(t *testing.T) {
	dir := t.TempDir()
	csv := "PersNr;Datum;von;bis\n77;17.08.2026;08:00;15:30\n78;17.08.2026;09:00;15:30\n"
	write(t, dir, "x.csv", []byte(csv))
	id := Identity{Username: "d.halko", Keys: []string{"d.halko", "77"}, ConfirmedKeys: []string{"77"}}
	bookings, res := importOptiTime(dir, id)
	if len(bookings) != 1 || res.Identity.MatchedPerson != "77" || res.Identity.AutoMatched {
		t.Fatalf("bookings=%d identity=%+v", len(bookings), res.Identity)
	}
}

func TestJSONImport(t *testing.T) {
	dir := t.TempDir()
	js := `{"buchungen":[{"Mitarbeiter":"Halko, David","Datum":"2026-08-17","Von":"08:00","Bis":"15:30"},{"Mitarbeiter":"X","Datum":"2026-08-17","Von":"08:00","Bis":"15:30"}]}`
	write(t, dir, "export.json", []byte(js))
	bookings, _ := importOptiTime(dir, ident())
	if len(bookings) != 1 {
		t.Fatalf("got %d", len(bookings))
	}
}

func TestFindOptiTimePath(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Programme/Opti-Time/data/a.csv", []byte("Datum;von;bis\n"))
	t.Setenv("OPTITIME_PATH", "")
	t.Setenv("USERPROFILE", dir)
	t.Setenv("APPDATA", "")
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("HOME", dir)
	t.Setenv("OneDrive", "")
	t.Setenv("ProgramData", "")
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramFiles(x86)", "")
	t.Setenv("PUBLIC", "")
	p, src, _ := findOptiTimePath("", "", "")
	if src != "gefunden" || filepath.Base(p) != "Opti-Time" {
		t.Fatalf("p=%q src=%q", p, src)
	}
	p2, src2, _ := findOptiTimePath(filepath.Join(dir, "Programme", "Opti-Time"), "", "")
	if src2 != "parameter" || p2 == "" {
		t.Fatalf("explicit: %q %q", p2, src2)
	}
	// Benutzerprofil C:\Users\<user>\AppData\Local\OptiTime hat Vorrang vor der allgemeinen Suche
	prof := filepath.Join(dir, "Users", "d.halko")
	write(t, prof, filepath.Join("AppData", "Local", "OptiTime", "stempel.csv"), []byte("Datum;von;bis\n"))
	p3, src3, _ := findOptiTimePath("", "", prof)
	if src3 != "benutzerprofil" || !strings.HasSuffix(p3, filepath.Join("AppData", "Local", "OptiTime")) {
		t.Fatalf("profil: %q %q", p3, src3)
	}
}

func TestMergeBookings(t *testing.T) {
	e := "12:00"
	existing := []Booking{{ID: "m1", Date: "2026-09-01", Start: "08:00", End: &e, Kind: kindWork}, {ID: "m2", Date: "2026-09-02", Start: "08:00", Kind: kindWork}}
	incoming := []Booking{{ID: "o1", Date: "2026-09-01", Start: "08:00", Kind: kindWork, Source: "optitime"}}
	out := mergeBookings(existing, incoming)
	if len(out) != 2 || out[0].ID != "o1" || out[1].ID != "m2" {
		t.Fatalf("merge: %+v", out)
	}
}
