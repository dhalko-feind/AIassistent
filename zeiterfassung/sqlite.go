package main

// sqlite.go – minimaler Leser für SQLite-Dateien (Format 3) ohne externe Module.
// Reicht, um Tabellen, Spaltennamen und Zeilen aus lokalen Client-Datenbanken
// (z. B. unter AppData\Local) zu lesen. Nur lesend, keine Indizes, keine WAL-Datei.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"unicode/utf16"
)

type sqliteDB struct {
	data     []byte
	pageSize int
	usable   int
	encoding int // 1 = UTF-8, 2 = UTF-16le, 3 = UTF-16be
	pages    int
}

type sqliteTable struct {
	Name     string
	SQL      string
	RootPage int
	Columns  []string
	RowidCol int // Index der INTEGER-PRIMARY-KEY-Spalte (Alias für rowid), sonst -1
}

var sqliteMagic = []byte("SQLite format 3\x00")

func isSQLite(b []byte) bool { return len(b) >= 16 && string(b[:16]) == string(sqliteMagic) }

func openSQLite(path string) (*sqliteDB, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !isSQLite(b) {
		return nil, errors.New("keine SQLite-Datei")
	}
	if len(b) < 100 {
		return nil, errors.New("SQLite-Datei zu kurz")
	}
	ps := int(binary.BigEndian.Uint16(b[16:18]))
	if ps == 1 {
		ps = 65536
	}
	if ps < 512 || ps&(ps-1) != 0 {
		return nil, fmt.Errorf("ungültige Seitengröße %d", ps)
	}
	db := &sqliteDB{data: b, pageSize: ps, usable: ps - int(b[20]), encoding: int(binary.BigEndian.Uint32(b[56:60])), pages: len(b) / ps}
	if db.encoding == 0 {
		db.encoding = 1
	}
	if db.usable < 480 {
		return nil, errors.New("ungültiger Seitenaufbau")
	}
	return db, nil
}

func (db *sqliteDB) page(n int) ([]byte, error) {
	if n < 1 || n > db.pages {
		return nil, fmt.Errorf("Seite %d außerhalb der Datei", n)
	}
	off := (n - 1) * db.pageSize
	return db.data[off : off+db.pageSize], nil
}

func varint(b []byte) (v int64, n int) {
	var u uint64
	for n < 9 && n < len(b) {
		c := b[n]
		n++
		if n == 9 {
			u = u<<8 | uint64(c)
			return int64(u), n
		}
		u = u<<7 | uint64(c&0x7f)
		if c&0x80 == 0 {
			return int64(u), n
		}
	}
	return int64(u), n
}

// payload liest den Zellinhalt inkl. Überlaufseiten.
func (db *sqliteDB) payload(cell []byte, total int, maxLocal, minLocal int) ([]byte, error) {
	if total <= maxLocal {
		if total > len(cell) {
			return nil, errors.New("Zelle beschädigt")
		}
		return cell[:total], nil
	}
	local := minLocal + (total-minLocal)%(db.usable-4)
	if local > maxLocal {
		local = minLocal
	}
	if local+4 > len(cell) {
		return nil, errors.New("Zelle beschädigt (Überlauf)")
	}
	out := make([]byte, 0, total)
	out = append(out, cell[:local]...)
	next := int(binary.BigEndian.Uint32(cell[local : local+4]))
	for len(out) < total && next != 0 {
		p, err := db.page(next)
		if err != nil {
			return nil, err
		}
		next = int(binary.BigEndian.Uint32(p[:4]))
		chunk := p[4:db.usable]
		if rem := total - len(out); rem < len(chunk) {
			chunk = chunk[:rem]
		}
		out = append(out, chunk...)
	}
	return out, nil
}

// walkTable ruft fn für jede Zeile der Tabelle mit Wurzelseite root auf.
func (db *sqliteDB) walkTable(root int, fn func(rowid int64, values []interface{}) bool) error {
	maxLocal := db.usable - 35
	minLocal := (db.usable-12)*32/255 - 23
	var visit func(pg, depth int) (bool, error)
	visit = func(pg, depth int) (bool, error) {
		if depth > 64 {
			return false, errors.New("B-Baum zu tief")
		}
		p, err := db.page(pg)
		if err != nil {
			return false, err
		}
		hdr := 0
		if pg == 1 {
			hdr = 100
		}
		if len(p) < hdr+8 {
			return false, errors.New("Seite zu kurz")
		}
		typ := p[hdr]
		ncell := int(binary.BigEndian.Uint16(p[hdr+3 : hdr+5]))
		switch typ {
		case 0x0D: // Blatt einer Tabelle
			ptrs := hdr + 8
			for i := 0; i < ncell; i++ {
				if ptrs+2*i+2 > len(p) {
					return false, errors.New("Zellzeiger außerhalb")
				}
				off := int(binary.BigEndian.Uint16(p[ptrs+2*i : ptrs+2*i+2]))
				if off >= len(p) {
					return false, errors.New("Zelle außerhalb")
				}
				cell := p[off:]
				plen, n1 := varint(cell)
				rowid, n2 := varint(cell[n1:])
				body, err := db.payload(cell[n1+n2:], int(plen), maxLocal, minLocal)
				if err != nil {
					return false, err
				}
				vals, err := db.record(body)
				if err != nil {
					return false, err
				}
				if !fn(rowid, vals) {
					return false, nil
				}
			}
			return true, nil
		case 0x05: // innere Seite einer Tabelle
			ptrs := hdr + 12
			for i := 0; i < ncell; i++ {
				off := int(binary.BigEndian.Uint16(p[ptrs+2*i : ptrs+2*i+2]))
				if off+4 > len(p) {
					return false, errors.New("Zelle außerhalb")
				}
				child := int(binary.BigEndian.Uint32(p[off : off+4]))
				cont, err := visit(child, depth+1)
				if err != nil || !cont {
					return cont, err
				}
			}
			right := int(binary.BigEndian.Uint32(p[hdr+8 : hdr+12]))
			return visit(right, depth+1)
		default:
			return false, fmt.Errorf("Seite %d ist keine Tabellenseite (Typ %#x)", pg, typ)
		}
	}
	_, err := visit(root, 0)
	return err
}

// record dekodiert einen SQLite-Datensatz (Kopf mit Serientypen, dann Werte).
func (db *sqliteDB) record(b []byte) ([]interface{}, error) {
	hlen, n := varint(b)
	if int(hlen) > len(b) || hlen < 1 {
		return nil, errors.New("Datensatzkopf beschädigt")
	}
	var types []int64
	pos := n
	for pos < int(hlen) {
		t, m := varint(b[pos:])
		types = append(types, t)
		pos += m
	}
	vals := make([]interface{}, 0, len(types))
	body := b[hlen:]
	off := 0
	need := func(k int) error {
		if off+k > len(body) {
			return errors.New("Datensatz beschädigt")
		}
		return nil
	}
	for _, t := range types {
		switch {
		case t == 0:
			vals = append(vals, nil)
		case t >= 1 && t <= 6:
			size := map[int64]int{1: 1, 2: 2, 3: 3, 4: 4, 5: 6, 6: 8}[t]
			if err := need(size); err != nil {
				return nil, err
			}
			var v int64
			for i := 0; i < size; i++ {
				v = v<<8 | int64(body[off+i])
			}
			shift := uint(64 - 8*size)
			v = v << shift >> shift // Vorzeichen
			vals = append(vals, v)
			off += size
		case t == 7:
			if err := need(8); err != nil {
				return nil, err
			}
			vals = append(vals, math.Float64frombits(binary.BigEndian.Uint64(body[off:off+8])))
			off += 8
		case t == 8:
			vals = append(vals, int64(0))
		case t == 9:
			vals = append(vals, int64(1))
		case t >= 12 && t%2 == 0:
			size := int((t - 12) / 2)
			if err := need(size); err != nil {
				return nil, err
			}
			vals = append(vals, append([]byte(nil), body[off:off+size]...))
			off += size
		case t >= 13:
			size := int((t - 13) / 2)
			if err := need(size); err != nil {
				return nil, err
			}
			vals = append(vals, db.text(body[off:off+size]))
			off += size
		default:
			return nil, fmt.Errorf("unbekannter Serientyp %d", t)
		}
	}
	return vals, nil
}

func (db *sqliteDB) text(b []byte) string {
	switch db.encoding {
	case 2, 3:
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			if db.encoding == 2 {
				u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
			} else {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			}
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}

// tables liest sqlite_master und liefert alle Tabellen mit Spaltennamen.
func (db *sqliteDB) tables() ([]sqliteTable, error) {
	var out []sqliteTable
	err := db.walkTable(1, func(_ int64, v []interface{}) bool {
		if len(v) < 5 {
			return true
		}
		typ, _ := v[0].(string)
		name, _ := v[1].(string)
		sql, _ := v[4].(string)
		root, _ := v[3].(int64)
		if typ == "table" && !strings.HasPrefix(name, "sqlite_") && root > 0 {
			cols, rowid := columnsFromCreate(sql)
			out = append(out, sqliteTable{Name: name, SQL: sql, RootPage: int(root), Columns: cols, RowidCol: rowid})
		}
		return true
	})
	return out, err
}

var constraintRe = regexp.MustCompile(`(?i)^(primary|unique|check|foreign|constraint)\b`)
var pkRe = regexp.MustCompile(`(?i)primary\s+key\s*\(\s*([^\s,)]+)`)

// identifier liest einen Bezeichner am Anfang von s (auch "a b", [a b] oder in Backticks).
func identifier(s string) (name, rest string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	var close byte
	switch s[0] {
	case '"', '\'', 0x60: // Anführungszeichen, Apostroph, Backtick
		close = s[0]
	case '[':
		close = ']'
	}
	if close != 0 {
		if j := strings.IndexByte(s[1:], close); j >= 0 {
			return s[1 : j+1], s[j+2:]
		}
	}
	f := strings.Fields(s)
	return f[0], strings.TrimSpace(s[len(f[0]):])
}

// columnsFromCreate zieht Spaltennamen aus einem CREATE-TABLE-Statement und
// erkennt die INTEGER-PRIMARY-KEY-Spalte (deren Wert in der Zeile als NULL steht).
func columnsFromCreate(sql string) (cols []string, rowid int) {
	rowid = -1
	i := strings.Index(sql, "(")
	j := strings.LastIndex(sql, ")")
	if i < 0 || j <= i {
		return nil, -1
	}
	inner := sql[i+1 : j]
	var defs []string
	depth, start := 0, 0
	for k, c := range inner {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				defs = append(defs, inner[start:k])
				start = k + 1
			}
		}
	}
	defs = append(defs, inner[start:])
	tablePK := ""
	for _, d := range defs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if constraintRe.MatchString(d) {
			if m := pkRe.FindStringSubmatch(d); m != nil {
				tablePK, _ = identifier(m[1])
			}
			continue
		}
		name, rest := identifier(d)
		cols = append(cols, name)
		up := strings.ToUpper(rest)
		if strings.HasPrefix(up, "INTEGER") && strings.Contains(up, "PRIMARY KEY") {
			rowid = len(cols) - 1
		}
	}
	if tablePK != "" && rowid < 0 {
		for k, c := range cols {
			if strings.EqualFold(c, tablePK) {
				// Typ prüfen
				for _, d := range defs {
					n, rest := identifier(d)
					if strings.EqualFold(n, c) && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(rest)), "INTEGER") {
						rowid = k
					}
				}
			}
		}
	}
	return cols, rowid
}

// rows liefert alle Zeilen einer Tabelle als Spaltenname -> Textwert (max. limit, 0 = alle).
func (db *sqliteDB) rows(t sqliteTable, limit int) ([]map[string]string, error) {
	var out []map[string]string
	err := db.walkTable(t.RootPage, func(rowid int64, v []interface{}) bool {
		m := map[string]string{"rowid": fmt.Sprint(rowid)}
		for i, c := range t.Columns {
			if i == t.RowidCol {
				m[c] = fmt.Sprint(rowid)
			} else if i < len(v) {
				m[c] = sqlValueString(v[i])
			}
		}
		out = append(out, m)
		return limit == 0 || len(out) < limit
	})
	return out, err
}

func sqlValueString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		if len(x) > 32 {
			return fmt.Sprintf("<blob %d bytes>", len(x))
		}
		return fmt.Sprintf("<blob %x>", x)
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	default:
		return fmt.Sprint(x)
	}
}
