// Stempeluhr – Zeiterfassung als Windows-Programm.
//
// Beim Start wird der angemeldete Benutzer ermittelt, der OptiTime-Ordner
// gesucht, die Buchungen dieses Benutzers eingelesen und die Oberfläche
// (index.html, app.js, zeit.js) über einen lokalen Server im Browser geöffnet.
// Die Daten liegen je Benutzer unter %APPDATA%\Stempeluhr\<Benutzer>\data.json.
package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed index.html app.js zeit.js seed-data.js
var webFS embed.FS

const version = "1.4.0"

const (
	kindWork  = "Arbeitszeit"
	kindBreak = "Pause"
)

type Booking struct {
	ID         string  `json:"id"`
	Date       string  `json:"date"`
	Start      string  `json:"start"`
	End        *string `json:"end"`
	Order      string  `json:"order"`
	Activity   string  `json:"activity"`
	Kind       string  `json:"kind"`
	Note       string  `json:"note"`
	Source     string  `json:"source,omitempty"`
	SourceFile string  `json:"sourceFile,omitempty"`
	Person     string  `json:"person,omitempty"`
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "b" + hex.EncodeToString(b)
}

func newBooking(typ, date, start string, end *string, note, file, person string) Booking {
	b := Booking{ID: newID(), Date: date, Start: start, End: end, Note: note, Source: "optitime", SourceFile: filepath.Base(file), Person: person}
	if typ == "break" {
		b.Order, b.Activity, b.Kind = "000007", "*Pause*", kindBreak
	} else {
		b.Order, b.Activity, b.Kind = "000999", "Kommen", kindWork
	}
	return b
}

// Profile: vom Benutzer bestätigte Kennungen für die Zuordnung in OptiTime-Dateien.
type Profile struct {
	Personalnummer string   `json:"personalnummer"`
	Name           string   `json:"name"`
	Keys           []string `json:"keys"`
}

type State struct {
	Bookings []Booking       `json:"bookings"`
	Settings json.RawMessage `json:"settings,omitempty"`
	Profile  Profile         `json:"profile"`
}

type Config struct {
	OptiTimePath string `json:"optitimePath,omitempty"`
	DataDir      string `json:"dataDir,omitempty"`
}

type App struct {
	mu        sync.Mutex
	cfg       Config
	cfgPath   string
	dataDir   string
	dataFile  string
	state     State
	id        Identity
	sync      SyncResult
	explicit  string
	lastPing  time.Time
	quit      chan struct{}
	logFile   string
	startedAt time.Time
}

// ---------- Benutzer und Pfade ----------

// currentIdentity ermittelt den Benutzer. override (Startparameter --user oder
// Umgebungsvariable STEMPELUHR_USER) hat Vorrang vor dem Windows-Konto.
func currentIdentity(override, source string) Identity {
	id := Identity{Source: "windows"}
	if override = strings.TrimSpace(override); override != "" {
		if i := strings.LastIndexAny(override, `\/`); i >= 0 {
			id.Domain = override[:i]
			override = override[i+1:]
		}
		id.Username = override
		id.Source = source
		if u, err := user.Lookup(override); err == nil {
			id.FullName = u.Name
		}
	} else if u, err := user.Current(); err == nil {
		name := u.Username
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			id.Domain = name[:i]
			name = name[i+1:]
		}
		id.Username = name
		id.FullName = u.Name
	}
	if id.Username == "" {
		id.Username = firstNonEmpty(os.Getenv("USERNAME"), os.Getenv("USER"), "unbekannt")
	}
	if id.Domain == "" {
		id.Domain = os.Getenv("USERDOMAIN")
	}
	id.Host, _ = os.Hostname()
	id.ProfileDir = profileDir(id.Username)
	return id
}

// profileDir liefert C:\Users\<Benutzer> (bzw. das Home-Verzeichnis) für den
// angegebenen Benutzer – auch wenn das Programm unter einem anderen Konto läuft.
func profileDir(username string) string {
	var cands []string
	cur := firstNonEmpty(os.Getenv("USERPROFILE"), os.Getenv("HOME"))
	if cur != "" && strings.EqualFold(filepath.Base(cur), username) {
		return cur
	}
	if cur != "" {
		cands = append(cands, filepath.Join(filepath.Dir(cur), username))
	}
	if sd := os.Getenv("SystemDrive"); sd != "" {
		cands = append(cands, filepath.Join(sd+`\`, "Users", username))
	}
	cands = append(cands, filepath.Join(`C:\Users`, username), filepath.Join("/home", username))
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return ""
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func baseDir() string {
	if v := os.Getenv("APPDATA"); v != "" {
		return filepath.Join(v, "Stempeluhr")
	}
	if h, err := os.UserConfigDir(); err == nil {
		return filepath.Join(h, "Stempeluhr")
	}
	return filepath.Join(".", "Stempeluhr")
}

func safeName(s string) string {
	var sb strings.Builder
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			sb.WriteRune(c)
		} else {
			sb.WriteRune('_')
		}
	}
	if sb.Len() == 0 {
		return "benutzer"
	}
	return sb.String()
}

// expandPath löst %VARIABLE% in Pfaden auf und entfernt Leerzeichen.
var winEnvRe = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_()]*)%`)

func expandPath(p string) string {
	p = winEnvRe.ReplaceAllStringFunc(strings.TrimSpace(p), func(m string) string {
		if v := os.Getenv(strings.Trim(m, "%")); v != "" {
			return v
		}
		return m
	})
	return strings.TrimSpace(strings.Trim(p, `"`))
}

// ensureWritable legt den Ordner an und prüft, ob wirklich geschrieben werden kann.
func ensureWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(dir, ".stempeluhr-schreibtest")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return err
	}
	return os.Remove(probe)
}

// userDataFile: <Ordner>\<Benutzer>\data.json – der Unterordner trennt die
// Benutzer, wenn mehrere denselben Ordner (OneDrive, Netzlaufwerk) verwenden.
func (a *App) userDataFile(dir string) string {
	return filepath.Join(dir, safeName(a.id.Username), "data.json")
}

// dataSuggestions liefert sinnvolle Ordner zur Auswahl in der Oberfläche.
func (a *App) dataSuggestions() []string {
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
	for _, env := range []string{"OneDriveCommercial", "OneDrive"} {
		if v := os.Getenv(env); v != "" {
			add(filepath.Join(v, "Stempeluhr"))
		}
	}
	if a.id.ProfileDir != "" {
		add(filepath.Join(a.id.ProfileDir, "Documents", "Stempeluhr"))
	}
	add(baseDir())
	return out
}

// switchDataDir wechselt den Datenordner. Vorhandene Buchungen werden
// übernommen; liegt im Zielordner bereits eine Datei, werden beide
// zusammengeführt. Die bisherige Datei bleibt als Sicherung liegen.
// Aufruf mit gehaltener Sperre.
func (a *App) switchDataDir(dir string) (string, error) {
	dir = expandPath(dir)
	if dir == "" {
		dir = baseDir()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("Pfad nicht verwendbar: %w", err)
	}
	dir = abs
	target := a.userDataFile(dir)
	if strings.EqualFold(target, a.dataFile) {
		return "Dieser Ordner ist bereits eingestellt.", nil
	}
	if err := ensureWritable(filepath.Dir(target)); err != nil {
		return "", fmt.Errorf("Ordner nicht beschreibbar: %w", err)
	}
	msg := fmt.Sprintf("%d Buchungen in den neuen Ordner übernommen.", len(a.state.Bookings))
	if b, rerr := os.ReadFile(target); rerr == nil {
		var existing State
		if json.Unmarshal(b, &existing) == nil {
			before := len(existing.Bookings)
			a.state.Bookings = mergeBookings(existing.Bookings, a.state.Bookings)
			msg = fmt.Sprintf("Im Zielordner lagen bereits %d Buchungen – zusammengeführt auf %d.", before, len(a.state.Bookings))
		}
	}
	oldDir, oldFile := a.dataDir, a.dataFile
	a.dataDir, a.dataFile = dir, target
	if err := a.saveState(); err != nil {
		a.dataDir, a.dataFile = oldDir, oldFile
		return "", fmt.Errorf("Speichern im neuen Ordner fehlgeschlagen: %w", err)
	}
	if dir == baseDir() {
		a.cfg.DataDir = ""
	} else {
		a.cfg.DataDir = dir
	}
	if err := a.saveConfig(); err != nil {
		log.Printf("Konfiguration speichern: %v", err)
	}
	if _, err := os.Stat(oldFile); err == nil {
		msg += " Die bisherige Datei bleibt als Sicherung unter " + oldFile + "."
	}
	log.Printf("Datenordner gewechselt: %s -> %s", oldFile, target)
	return msg, nil
}

// ---------- Laden / Speichern ----------

func (a *App) loadConfig() {
	b, err := os.ReadFile(a.cfgPath)
	if err == nil {
		_ = json.Unmarshal(b, &a.cfg)
	}
}

func (a *App) saveConfig() error {
	if err := os.MkdirAll(filepath.Dir(a.cfgPath), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(a.cfg, "", "  ")
	return os.WriteFile(a.cfgPath, b, 0o644)
}

func (a *App) loadState() {
	a.state = State{Bookings: []Booking{}}
	b, err := os.ReadFile(a.dataFile)
	if err != nil {
		return
	}
	var s State
	if json.Unmarshal(b, &s) == nil {
		if s.Bookings == nil {
			s.Bookings = []Booking{}
		}
		a.state = s
	}
}

func (a *App) saveState() error {
	if err := os.MkdirAll(filepath.Dir(a.dataFile), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.dataFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.dataFile)
}

// identityKeys: Windows-Konto plus bestätigte Kennungen aus dem Profil.
func (a *App) identityWithProfile() Identity {
	id := a.id
	id.Keys = nil
	id.MatchedPerson, id.AutoMatched = "", false
	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" {
			return
		}
		for _, e := range id.Keys {
			if strings.EqualFold(e, k) {
				return
			}
		}
		id.Keys = append(id.Keys, k)
	}
	add(id.Username)
	add(id.FullName)
	for _, k := range append([]string{a.state.Profile.Personalnummer, a.state.Profile.Name}, a.state.Profile.Keys...) {
		add(k)
		if k = strings.TrimSpace(k); k != "" {
			id.ConfirmedKeys = append(id.ConfirmedKeys, k)
		}
	}
	return id
}

// mergeBookings übernimmt OptiTime-Buchungen: gleiche Kombination Datum+Start
// wird ersetzt (OptiTime ist führend), alles andere bleibt erhalten.
func mergeBookings(existing, incoming []Booking) []Booking {
	key := func(b Booking) string { return b.Date + " " + b.Start }
	seen := map[string]bool{}
	for _, b := range incoming {
		seen[key(b)] = true
	}
	out := make([]Booking, 0, len(existing)+len(incoming))
	for _, b := range existing {
		if !seen[key(b)] {
			out = append(out, b)
		}
	}
	out = append(out, incoming...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Start < out[j].Start
	})
	return out
}

// runSync sucht den OptiTime-Pfad und liest die Buchungen des Benutzers ein.
func (a *App) runSync() {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := a.identityWithProfile()
	path, source, searched := findOptiTimePath(a.explicit, a.cfg.OptiTimePath, id.ProfileDir)
	if path == "" {
		a.sync = SyncResult{At: time.Now(), Searched: searched, Identity: id, Files: []FileResult{}, Persons: []string{}, Errors: []string{"Kein OptiTime-Ordner gefunden. Pfad in den Einstellungen angeben."}}
		log.Printf("OptiTime: kein Ordner gefunden (%d Orte geprüft)", len(searched))
		return
	}
	bookings, res := importOptiTime(path, id)
	res.PathSource, res.Searched = source, searched
	a.sync = res
	if len(bookings) > 0 {
		a.state.Bookings = mergeBookings(a.state.Bookings, bookings)
		if err := a.saveState(); err != nil {
			a.sync.Errors = append(a.sync.Errors, "Speichern fehlgeschlagen: "+err.Error())
		}
	}
	log.Printf("OptiTime: %s (%s), %d Dateien, %d Buchungen für %q übernommen", path, source, len(res.Files), len(bookings), res.Identity.MatchedPerson)
}

// ---------- HTTP ----------

func (a *App) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *App) info() map[string]interface{} {
	return map[string]interface{}{
		"mode": "server", "version": version, "user": a.id, "dataFile": a.dataFile, "configFile": a.cfgPath,
		"dataDir": a.dataDir, "defaultDataDir": baseDir(), "dataSuggestions": a.dataSuggestions(),
		"logFile": a.logFile, "optitime": a.sync, "startedAt": a.startedAt,
	}
}

func (a *App) handleInfo(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writeJSON(w, a.info())
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		a.writeJSON(w, a.state)
	case http.MethodPut:
		var s State
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 20<<20)).Decode(&s); err != nil {
			http.Error(w, "ungültiges JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if s.Bookings == nil {
			s.Bookings = []Booking{}
		}
		a.state = s
		if err := a.saveState(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.writeJSON(w, map[string]interface{}{"ok": true, "saved": time.Now()})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.runSync()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writeJSON(w, map[string]interface{}{"info": a.info(), "state": a.state})
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		OptiTimePath   *string  `json:"optitimePath"`
		Personalnummer *string  `json:"personalnummer"`
		Name           *string  `json:"name"`
		Keys           []string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "ungültiges JSON", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	if in.OptiTimePath != nil {
		a.cfg.OptiTimePath = strings.TrimSpace(*in.OptiTimePath)
		a.explicit = "" // manuelle Angabe hat Vorrang vor dem Startparameter
		if err := a.saveConfig(); err != nil {
			log.Printf("Konfiguration speichern: %v", err)
		}
	}
	if in.Personalnummer != nil {
		a.state.Profile.Personalnummer = strings.TrimSpace(*in.Personalnummer)
	}
	if in.Name != nil {
		a.state.Profile.Name = strings.TrimSpace(*in.Name)
	}
	if in.Keys != nil {
		a.state.Profile.Keys = in.Keys
	}
	_ = a.saveState()
	a.mu.Unlock()
	a.runSync()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writeJSON(w, map[string]interface{}{"info": a.info(), "state": a.state})
}

func (a *App) handleDataDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		DataDir string `json:"dataDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "ungültiges JSON", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	msg, err := a.switchDataDir(in.DataDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeJSON(w, map[string]interface{}{"info": a.info(), "state": a.state, "message": msg})
}

// handleOpen öffnet einen der bekannten Ordner im Explorer – keine freien Pfade.
func (a *App) handleOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	var target string
	switch r.URL.Query().Get("was") {
	case "optitime":
		target = a.sync.Path
	case "protokoll":
		target = filepath.Dir(a.logFile)
	default:
		target = filepath.Dir(a.dataFile)
	}
	a.mu.Unlock()
	if target == "" {
		http.Error(w, "Kein Ordner vorhanden", http.StatusNotFound)
		return
	}
	if err := openFolder(target); err != nil {
		http.Error(w, "Ordner konnte nicht geöffnet werden: "+err.Error(), http.StatusInternalServerError)
		return
	}
	a.writeJSON(w, map[string]interface{}{"ok": true, "path": target})
}

func (a *App) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	report := a.diagnose()
	_ = os.WriteFile(filepath.Join(filepath.Dir(a.logFile), "diagnose.txt"), []byte(report), 0o644)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(report))
}

func (a *App) handlePing(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.lastPing = time.Now()
	a.mu.Unlock()
	a.writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handleQuit(w http.ResponseWriter, r *http.Request) {
	a.writeJSON(w, map[string]bool{"ok": true})
	go func() { time.Sleep(300 * time.Millisecond); close(a.quit) }()
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/info", a.handleInfo)
	mux.HandleFunc("/api/state", a.handleState)
	mux.HandleFunc("/api/optitime/sync", a.handleSync)
	mux.HandleFunc("/api/config", a.handleConfig)
	mux.HandleFunc("/api/optitime/diagnose", a.handleDiagnose)
	mux.HandleFunc("/api/datadir", a.handleDataDir)
	mux.HandleFunc("/api/open", a.handleOpen)
	mux.HandleFunc("/api/ping", a.handlePing)
	mux.HandleFunc("/api/quit", a.handleQuit)
	sub, _ := fs.Sub(webFS, ".")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
}

// ---------- Start ----------

func main() {
	userFlag := flag.String("user", "", "Benutzer, dessen Daten gelesen werden (Standard: angemeldeter Windows-Benutzer; auch STEMPELUHR_USER)")
	optiFlag := flag.String("optitime", "", "Pfad zum OptiTime-Ordner (überschreibt Suche und Konfiguration)")
	dataFlag := flag.String("data", "", "Ordner für die Benutzerdaten (Standard: %APPDATA%\\Stempeluhr)")
	portFlag := flag.Int("port", 0, "fester Port (Standard: freier Port)")
	noBrowser := flag.Bool("no-browser", false, "Browser nicht automatisch öffnen")
	idle := flag.Duration("idle", 2*time.Minute, "Beenden, wenn so lange kein Browserfenster mehr offen ist (0 = nie)")
	diag := flag.Bool("diagnose", false, "Diagnosebericht schreiben (%APPDATA%\\Stempeluhr\\diagnose.txt) und beenden")
	flag.Parse()

	a := &App{explicit: *optiFlag, quit: make(chan struct{}), startedAt: time.Now(), lastPing: time.Now()}
	userOverride, userSource := *userFlag, "parameter"
	if userOverride == "" {
		userOverride, userSource = os.Getenv("STEMPELUHR_USER"), "umgebung"
	}
	a.id = currentIdentity(userOverride, userSource)
	dir := baseDir()
	a.cfgPath = filepath.Join(dir, "config.json")
	a.loadConfig()
	if *dataFlag != "" {
		a.cfg.DataDir = *dataFlag
	}
	a.dataDir = dir
	if a.cfg.DataDir != "" {
		if abs, err := filepath.Abs(expandPath(a.cfg.DataDir)); err == nil {
			a.dataDir = abs
		}
	}
	a.dataFile = a.userDataFile(a.dataDir)
	a.logFile = filepath.Join(dir, "stempeluhr.log")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		if f, err := os.OpenFile(a.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			log.SetOutput(f)
		}
	}
	log.Printf("Stempeluhr %s startet für %s\\%s (%s, Quelle: %s, Profil: %s) auf %s", version, a.id.Domain, a.id.Username, a.id.FullName, a.id.Source, a.id.ProfileDir, a.id.Host)
	a.loadState()
	a.runSync()
	if *diag {
		report := a.diagnose()
		out := filepath.Join(dir, "diagnose.txt")
		if err := os.WriteFile(out, []byte(report), 0o644); err != nil {
			fatal("Diagnose konnte nicht geschrieben werden: " + err.Error())
		}
		fmt.Println(report)
		showMessage("Stempeluhr – Diagnose", "Bericht gespeichert unter:\n"+out+"\n\nBitte diese Datei weitergeben.")
		return
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *portFlag))
	if err != nil {
		fatal("Stempeluhr kann keinen lokalen Port öffnen:\n" + err.Error())
	}
	url := "http://" + ln.Addr().String() + "/"
	srv := &http.Server{Handler: a.routes()}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fatal("Server beendet: " + err.Error())
		}
	}()
	log.Printf("Oberfläche: %s", url)
	if !*noBrowser {
		if err := openBrowser(url); err != nil {
			showMessage("Stempeluhr", "Browser konnte nicht geöffnet werden.\nBitte manuell aufrufen:\n"+url)
		}
	}
	if *idle > 0 {
		go func() {
			t := time.NewTicker(10 * time.Second)
			defer t.Stop()
			for range t.C {
				a.mu.Lock()
				since := time.Since(a.lastPing)
				a.mu.Unlock()
				if since > *idle {
					log.Printf("Kein Browserfenster seit %s – beende.", since.Round(time.Second))
					close(a.quit)
					return
				}
			}
		}()
	}
	<-a.quit
	_ = srv.Close()
	log.Printf("Stempeluhr beendet.")
}

func fatal(msg string) {
	log.Print(msg)
	showMessage("Stempeluhr – Fehler", msg)
	os.Exit(1)
}
