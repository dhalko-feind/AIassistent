package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApp(t *testing.T, base string) *App {
	t.Helper()
	t.Setenv("APPDATA", base)
	a := &App{id: Identity{Username: "d.halko"}}
	a.cfgPath = filepath.Join(baseDir(), "config.json")
	a.logFile = filepath.Join(baseDir(), "stempeluhr.log")
	a.dataDir = baseDir()
	a.dataFile = a.userDataFile(a.dataDir)
	a.state = State{Bookings: []Booking{{ID: "m1", Date: "2026-09-01", Start: "08:00", Kind: kindWork}}}
	if err := a.saveState(); err != nil {
		t.Fatal(err)
	}
	return a
}

func readState(t *testing.T, path string) State {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExpandPath(t *testing.T) {
	t.Setenv("OneDrive", filepath.Join("C:", "Users", "d.halko", "OneDrive"))
	got := expandPath(`  "%OneDrive%\Stempeluhr"  `)
	want := filepath.Join("C:", "Users", "d.halko", "OneDrive") + `\Stempeluhr`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if expandPath("%GIBTESNICHT%") != "%GIBTESNICHT%" {
		t.Fatal("unbekannte Variable darf nicht verschwinden")
	}
	if expandPath("   ") != "" {
		t.Fatal("leerer Pfad")
	}
}

func TestSwitchDataDirMigriert(t *testing.T) {
	base := t.TempDir()
	a := testApp(t, base)
	oldFile := a.dataFile
	target := filepath.Join(base, "OneDrive", "Stempeluhr")

	msg, err := a.switchDataDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if a.dataFile != filepath.Join(target, "d.halko", "data.json") {
		t.Fatalf("neue Datei: %s", a.dataFile)
	}
	if s := readState(t, a.dataFile); len(s.Bookings) != 1 || s.Bookings[0].ID != "m1" {
		t.Fatalf("Buchungen nicht übernommen: %+v", s.Bookings)
	}
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatal("bisherige Datei muss als Sicherung bleiben")
	}
	if !strings.Contains(msg, "Sicherung") {
		t.Fatalf("Meldung: %q", msg)
	}
	if a.cfg.DataDir != target {
		t.Fatalf("Konfiguration: %q", a.cfg.DataDir)
	}
	// Konfiguration wurde geschrieben und wird beim nächsten Start gelesen
	b := &App{id: a.id, cfgPath: a.cfgPath}
	b.loadConfig()
	if b.cfg.DataDir != target {
		t.Fatalf("config.json: %q", b.cfg.DataDir)
	}

	// Zurück auf Standard: cfg leer, damit der Standardordner gilt
	if _, err := a.switchDataDir(""); err != nil {
		t.Fatal(err)
	}
	if a.cfg.DataDir != "" || a.dataDir != baseDir() {
		t.Fatalf("Standard: cfg=%q dir=%q", a.cfg.DataDir, a.dataDir)
	}
}

func TestSwitchDataDirFuehrtZusammen(t *testing.T) {
	base := t.TempDir()
	a := testApp(t, base)
	target := filepath.Join(base, "Netz")
	// Im Zielordner liegt bereits eine Datei mit anderer Buchung
	pre := State{Bookings: []Booking{{ID: "alt", Date: "2026-08-31", Start: "07:00", Kind: kindWork}}}
	if err := os.MkdirAll(filepath.Join(target, "d.halko"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(pre)
	if err := os.WriteFile(filepath.Join(target, "d.halko", "data.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	msg, err := a.switchDataDir(target)
	if err != nil {
		t.Fatal(err)
	}
	s := readState(t, a.dataFile)
	if len(s.Bookings) != 2 || s.Bookings[0].ID != "alt" || s.Bookings[1].ID != "m1" {
		t.Fatalf("Zusammenführung: %+v", s.Bookings)
	}
	if !strings.Contains(msg, "zusammengeführt") {
		t.Fatalf("Meldung: %q", msg)
	}
	// Derselbe Ordner erneut: keine Änderung, keine Dopplung
	if msg2, err := a.switchDataDir(target); err != nil || !strings.Contains(msg2, "bereits") {
		t.Fatalf("erneut: %q %v", msg2, err)
	}
	if s2 := readState(t, a.dataFile); len(s2.Bookings) != 2 {
		t.Fatalf("Dopplung: %+v", s2.Bookings)
	}
}

func TestSwitchDataDirFehlerBehaeltAlten(t *testing.T) {
	base := t.TempDir()
	a := testApp(t, base)
	old := a.dataFile
	// Datei statt Ordner als Ziel -> muss scheitern, alter Stand bleibt
	blocker := filepath.Join(base, "blockiert")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.switchDataDir(blocker); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if a.dataFile != old {
		t.Fatalf("Pfad wurde trotz Fehler geändert: %s", a.dataFile)
	}
}

func TestDataSuggestions(t *testing.T) {
	base := t.TempDir()
	a := testApp(t, base)
	t.Setenv("OneDrive", filepath.Join(base, "OneDrive"))
	a.id.ProfileDir = filepath.Join(base, "Users", "d.halko")
	got := a.dataSuggestions()
	if len(got) != 3 || got[0] != filepath.Join(base, "OneDrive", "Stempeluhr") || got[2] != baseDir() {
		t.Fatalf("Vorschläge: %v", got)
	}
}
