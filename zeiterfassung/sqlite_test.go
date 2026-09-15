package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Testdatenbank mit python3/sqlite3 erzeugen: zwei Tabellen, viele Zeilen (innere Seiten),
// ein langer Text (Überlaufseite).
func makeTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db := filepath.Join(dir, "optitime.db")
	script := `
import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.execute("CREATE TABLE Buchungen (ID INTEGER PRIMARY KEY, Personalnummer TEXT, Datum TEXT, Uhrzeit TEXT, Buchungsart TEXT, Bemerkung TEXT)")
c.execute("CREATE TABLE \"Mitarbeiter\" ([Nr] INTEGER, Name TEXT, PRIMARY KEY(Nr))")
rows = []
for i in range(3000):
    d = "2026-%02d-%02d" % (1 + i // 250, 1 + (i % 28))
    rows.append((str(1042 if i % 2 == 0 else 2001), d, "%02d:%02d" % (i % 24, i % 60), "Kommen" if i % 2 == 0 else "Gehen", ""))
c.executemany("INSERT INTO Buchungen (Personalnummer, Datum, Uhrzeit, Buchungsart, Bemerkung) VALUES (?,?,?,?,?)", rows)
c.execute("INSERT INTO Buchungen (Personalnummer, Datum, Uhrzeit, Buchungsart, Bemerkung) VALUES ('1042','2026-09-11','06:54','Kommen',?)", ("x" * 9000,))
c.execute("INSERT INTO Mitarbeiter VALUES (1042, 'Halko, David')")
c.execute("INSERT INTO Mitarbeiter VALUES (2001, 'Muster, Erika')")
c.commit()
`
	cmd := exec.Command("python3", "-c", script, db)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("python3/sqlite3 nicht verfügbar: %v %s", err, out)
	}
	return db
}

func TestSQLiteReader(t *testing.T) {
	path := makeTestDB(t)
	db, err := openSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := db.tables()
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 {
		t.Fatalf("Tabellen: %+v", tables)
	}
	var buch, ma sqliteTable
	for _, tb := range tables {
		if tb.Name == "Buchungen" {
			buch = tb
		} else {
			ma = tb
		}
	}
	if strings.Join(buch.Columns, ",") != "ID,Personalnummer,Datum,Uhrzeit,Buchungsart,Bemerkung" {
		t.Fatalf("Spalten: %v", buch.Columns)
	}
	if strings.Join(ma.Columns, ",") != "Nr,Name" {
		t.Fatalf("Spalten Mitarbeiter: %v", ma.Columns)
	}
	rows, err := db.rows(buch, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3001 {
		t.Fatalf("Zeilen: %d", len(rows))
	}
	last := rows[len(rows)-1]
	if last["Datum"] != "2026-09-11" || len(last["Bemerkung"]) != 9000 {
		t.Fatalf("Überlaufzeile: %v %d", last["Datum"], len(last["Bemerkung"]))
	}
	if rows[0]["Personalnummer"] != "1042" || rows[0]["Buchungsart"] != "Kommen" {
		t.Fatalf("erste Zeile: %v", rows[0])
	}
	if mrows, _ := db.rows(ma, 0); len(mrows) != 2 || mrows[0]["Name"] != "Halko, David" || mrows[0]["Nr"] != "1042" {
		t.Fatalf("Mitarbeiter: %v", mrows)
	}
}

func TestSQLiteImport(t *testing.T) {
	path := makeTestDB(t)
	id := Identity{Username: "d.halko", Keys: []string{"d.halko", "1042"}, ConfirmedKeys: []string{"1042"}}
	bookings, res := importOptiTime(filepath.Dir(path), id)
	if len(res.Files) != 1 || !strings.HasPrefix(res.Files[0].Format, "sqlite") {
		t.Fatalf("files: %+v", res.Files)
	}
	if res.Files[0].Rows != 3001 || res.Files[0].Skipped != 1500 || res.Files[0].Taken == 0 {
		t.Fatalf("rows/skipped: %+v", res.Files[0])
	}
	// Personalnummer 1042 wird über die Tabelle Mitarbeiter zum Namen aufgelöst
	if len(bookings) == 0 || res.Identity.MatchedPerson != "Halko, David" || res.Identity.AutoMatched {
		t.Fatalf("bookings=%d identity=%+v", len(bookings), res.Identity)
	}
	for _, b := range bookings {
		if b.Person != "Halko, David" {
			t.Fatalf("fremde Buchung: %+v", b)
		}
	}
	if len(res.Persons) != 2 || res.Persons[0] != "Halko, David" {
		t.Fatalf("persons: %v", res.Persons)
	}
}

func TestColumnsFromCreate(t *testing.T) {
	got, rowid := columnsFromCreate("CREATE TABLE t(\"a b\" TEXT, [c] INT DEFAULT (1+2), d REAL, PRIMARY KEY(d), FOREIGN KEY(c) REFERENCES x(y))")
	if strings.Join(got, "|") != "a b|c|d" || rowid != -1 {
		t.Fatalf("%v %d", got, rowid)
	}
	got2, rowid2 := columnsFromCreate("CREATE TABLE u(ID INTEGER PRIMARY KEY AUTOINCREMENT, Name TEXT)")
	if strings.Join(got2, "|") != "ID|Name" || rowid2 != 0 {
		t.Fatalf("%v %d", got2, rowid2)
	}
}

func TestEpochValues(t *testing.T) {
	m := map[string]string{"Zeitstempel": "1788764040", "Buchungsart": "Kommen", "Benutzer": "d.halko"} // 2026-09-07 06:54 UTC
	e, ok := entryFromMap(m)
	if !ok || e.date != "2026-09-07" || e.start == "" {
		t.Fatalf("epoch: %+v ok=%v", e, ok)
	}
}
