/*
 * zeit.js – Berechnungslogik der Zeiterfassung (ohne DOM).
 *
 * Läuft im Browser (window.Zeit) und in Node (module.exports), damit die
 * Regeln in test/zeit.test.js ohne Browser geprüft werden können.
 *
 * Datenmodell einer Buchung:
 *   { id, date: 'YYYY-MM-DD', start: 'HH:MM', end: 'HH:MM' | null,
 *     order: '000999', activity: 'Kommen' | '*Pause*',
 *     kind: 'Arbeitszeit' | 'Pause', note: '' }
 */
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.Zeit = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const KIND_WORK = 'Arbeitszeit';
  const KIND_BREAK = 'Pause';

  const ACTIVITIES = {
    work: { order: '000999', activity: 'Kommen', kind: KIND_WORK },
    break: { order: '000007', activity: '*Pause*', kind: KIND_BREAK },
  };

  const DEFAULT_SETTINGS = {
    weeklyHours: 40,
    workDays: [1, 2, 3, 4, 5], // ISO: 1 = Montag … 7 = Sonntag
    roundingMinutes: 1,
  };

  // ---------- Zeit-Helfer ----------

  function pad(n) { return String(n).padStart(2, '0'); }

  function parseHM(str) {
    if (str == null || str === '') return null;
    const m = /^(\d{1,2}):(\d{2})$/.exec(String(str).trim());
    if (!m) return null;
    const h = Number(m[1]), min = Number(m[2]);
    if (h > 23 || min > 59) return null;
    return h * 60 + min;
  }

  function formatHM(minutes) {
    if (minutes == null || Number.isNaN(minutes)) return '';
    const neg = minutes < 0;
    const abs = Math.abs(Math.round(minutes));
    return (neg ? '-' : '') + pad(Math.floor(abs / 60)) + ':' + pad(abs % 60);
  }

  // Dauer als "h:mm" (ohne führende Null bei Stunden), Vorzeichen für Überstunden.
  function formatDuration(minutes, { signed = false } = {}) {
    if (minutes == null || Number.isNaN(minutes)) return '–';
    const abs = Math.abs(Math.round(minutes));
    const body = Math.floor(abs / 60) + ':' + pad(abs % 60);
    if (minutes < 0) return '-' + body;
    return (signed && minutes > 0 ? '+' : '') + body;
  }

  function parseISODate(str) {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(str || ''));
    if (!m) return null;
    return new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
  }

  function toISODate(d) {
    return d.getUTCFullYear() + '-' + pad(d.getUTCMonth() + 1) + '-' + pad(d.getUTCDate());
  }

  // Lokales Datum (Browser-Zeitzone) als ISO-String.
  function todayISO(now = new Date()) {
    return now.getFullYear() + '-' + pad(now.getMonth() + 1) + '-' + pad(now.getDate());
  }

  function nowHM(now = new Date()) {
    return pad(now.getHours()) + ':' + pad(now.getMinutes());
  }

  // "17.08.2026" -> "2026-08-17"
  function fromGermanDate(str) {
    const m = /^(\d{1,2})\.(\d{1,2})\.(\d{4})$/.exec(String(str || '').trim());
    if (!m) return null;
    return m[3] + '-' + pad(m[2]) + '-' + pad(m[1]);
  }

  function toGermanDate(iso) {
    const d = parseISODate(iso);
    if (!d) return '';
    return pad(d.getUTCDate()) + '.' + pad(d.getUTCMonth() + 1) + '.' + d.getUTCFullYear();
  }

  const WEEKDAYS_DE = ['So', 'Mo', 'Di', 'Mi', 'Do', 'Fr', 'Sa'];

  function weekdayShort(iso) {
    const d = parseISODate(iso);
    return d ? WEEKDAYS_DE[d.getUTCDay()] : '';
  }

  // ISO-Wochentag: 1 = Montag … 7 = Sonntag
  function isoWeekday(iso) {
    const d = parseISODate(iso);
    if (!d) return null;
    const day = d.getUTCDay();
    return day === 0 ? 7 : day;
  }

  // ISO-8601-Kalenderwoche, liefert { year, week, key: 'YYYY-Www' }
  function isoWeek(iso) {
    const d = parseISODate(iso);
    if (!d) return null;
    const t = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
    const dayNum = t.getUTCDay() || 7;
    t.setUTCDate(t.getUTCDate() + 4 - dayNum);
    const yearStart = new Date(Date.UTC(t.getUTCFullYear(), 0, 1));
    const week = Math.ceil(((t - yearStart) / 86400000 + 1) / 7);
    const year = t.getUTCFullYear();
    return { year, week, key: year + '-W' + pad(week) };
  }

  // Montag der Woche als ISO-Datum
  function weekStart(iso) {
    const d = parseISODate(iso);
    if (!d) return null;
    const wd = d.getUTCDay() || 7;
    d.setUTCDate(d.getUTCDate() - (wd - 1));
    return toISODate(d);
  }

  function addDays(iso, n) {
    const d = parseISODate(iso);
    if (!d) return null;
    d.setUTCDate(d.getUTCDate() + n);
    return toISODate(d);
  }

  // ---------- Buchungen ----------

  function newId() {
    return 'b' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
  }

  function makeBooking(type, date, start, end = null, note = '') {
    const a = ACTIVITIES[type];
    if (!a) throw new Error('Unbekannter Buchungstyp: ' + type);
    return { id: newId(), date, start, end, order: a.order, activity: a.activity, kind: a.kind, note };
  }

  // Dauer einer Buchung in Minuten. Offene Buchungen: bis `nowMinutes`, wenn
  // die Buchung heute ist, sonst null.
  function bookingMinutes(b, { today, nowMinutes } = {}) {
    const s = parseHM(b.start);
    if (s == null) return null;
    let e = parseHM(b.end);
    if (e == null) {
      // Laufende Buchung: bis jetzt. Liegt der Start nach der aktuellen Uhrzeit
      // (Uhr des Terminals weicht ab), ist noch keine Zeit vergangen.
      if (today && b.date === today && nowMinutes != null) return Math.max(0, nowMinutes - s);
      return null;
    }
    if (e < s) e += 24 * 60; // über Mitternacht
    return e - s;
  }

  function validateBooking(b) {
    const errors = [];
    if (!parseISODate(b.date)) errors.push('Datum ungültig.');
    if (parseHM(b.start) == null) errors.push('Startzeit ungültig (HH:MM).');
    if (b.end != null && b.end !== '' && parseHM(b.end) == null) errors.push('Endzeit ungültig (HH:MM).');
    if (b.kind !== KIND_WORK && b.kind !== KIND_BREAK) errors.push('Zeitart muss Arbeitszeit oder Pause sein.');
    return errors;
  }

  function sortBookings(list) {
    return [...list].sort((a, b) =>
      a.date === b.date ? (parseHM(a.start) ?? 0) - (parseHM(b.start) ?? 0) : (a.date < b.date ? -1 : 1));
  }

  // ---------- Auswertung ----------

  // Arbeitszeitgesetz (§ 3, § 4 ArbZG) – Hinweise, keine Rechtsberatung.
  function legalHints(workMin, breakMin) {
    const hints = [];
    if (workMin > 6 * 60 && breakMin < 30) hints.push('Bei mehr als 6 h Arbeit sind mind. 30 min Pause vorgesehen (§ 4 ArbZG).');
    if (workMin > 9 * 60 && breakMin < 45) hints.push('Bei mehr als 9 h Arbeit sind mind. 45 min Pause vorgesehen (§ 4 ArbZG).');
    if (workMin > 10 * 60) hints.push('Mehr als 10 h Arbeitszeit an einem Tag (§ 3 ArbZG).');
    return hints;
  }

  function dailyTarget(settings) {
    const s = { ...DEFAULT_SETTINGS, ...settings };
    const days = s.workDays.length || 5;
    return Math.round((s.weeklyHours * 60) / days);
  }

  // Auswertung eines Tages.
  function computeDay(date, bookings, settings = {}, ctx = {}) {
    const list = sortBookings(bookings.filter(b => b.date === date));
    let workMin = 0, breakMin = 0;
    let open = 0, firstIn = null, lastOut = null, running = null;
    const breaks = [];

    for (const b of list) {
      const s = parseHM(b.start);
      if (s != null && (firstIn == null || s < firstIn)) firstIn = s;
      const e = parseHM(b.end);
      if (e == null) { open++; running = b; } else if (lastOut == null || e > lastOut) lastOut = e;
      const min = bookingMinutes(b, ctx);
      if (min == null) continue;
      if (b.kind === KIND_BREAK) { breakMin += min; breaks.push({ start: b.start, end: b.end, minutes: min }); }
      else workMin += min;
    }

    const isWorkDay = ({ ...DEFAULT_SETTINGS, ...settings }).workDays.includes(isoWeekday(date));
    const target = list.length && isWorkDay ? dailyTarget(settings) : 0;
    const hints = legalHints(workMin, breakMin);
    const openPast = open > 0 && ctx.today && date < ctx.today;
    if (openPast) hints.unshift('Buchung nicht ausgestempelt – bitte Endzeit nachtragen.');

    return {
      date,
      week: isoWeek(date),
      bookings: list,
      firstIn: firstIn == null ? null : formatHM(firstIn),
      lastOut: open > 0 ? null : (lastOut == null ? null : formatHM(lastOut)),
      breaks,
      workMin, breakMin, grossMin: workMin + breakMin,
      open, running, openPast: !!openPast,
      target, overtimeMin: workMin - target,
      hints,
    };
  }

  // Alle Tage mit Buchungen, absteigend (neueste zuerst).
  function computeDays(bookings, settings = {}, ctx = {}) {
    const dates = [...new Set(bookings.map(b => b.date))].sort().reverse();
    return dates.map(d => computeDay(d, bookings, settings, ctx));
  }

  // Wochenauswertung auf Basis der Tagesauswertungen.
  function computeWeeks(days) {
    const map = new Map();
    for (const d of days) {
      if (!d.week) continue;
      const k = d.week.key;
      if (!map.has(k)) {
        map.set(k, { key: k, year: d.week.year, week: d.week.week, start: weekStart(d.date),
          days: [], workMin: 0, breakMin: 0, target: 0, overtimeMin: 0, open: 0, hints: 0 });
      }
      const w = map.get(k);
      w.days.push(d);
      w.workMin += d.workMin; w.breakMin += d.breakMin; w.target += d.target;
      w.overtimeMin += d.overtimeMin; w.open += d.open; w.hints += d.hints.length;
    }
    return [...map.values()].sort((a, b) => (a.key < b.key ? 1 : -1))
      .map(w => ({ ...w, end: addDays(w.start, 6), days: w.days.sort((a, b) => (a.date < b.date ? -1 : 1)) }));
  }

  function computeTotals(days) {
    return days.reduce((t, d) => ({
      workMin: t.workMin + d.workMin,
      breakMin: t.breakMin + d.breakMin,
      grossMin: t.grossMin + d.grossMin,
      overtimeMin: t.overtimeMin + d.overtimeMin,
      open: t.open + d.open,
      days: t.days + 1,
    }), { workMin: 0, breakMin: 0, grossMin: 0, overtimeMin: 0, open: 0, days: 0 });
  }

  // ---------- Stempeluhr ----------

  // Zustand der Stempeluhr für heute: 'out' | 'working' | 'break'
  function clockState(bookings, today) {
    const open = bookings.find(b => b.date === today && (b.end == null || b.end === ''));
    if (!open) return { state: 'out', open: null };
    return { state: open.kind === KIND_BREAK ? 'break' : 'working', open };
  }

  // Führt eine Stempelaktion aus und liefert die neue Buchungsliste.
  // action: 'in' | 'break' | 'resume' | 'out'
  function punch(bookings, action, today, time) {
    const { state, open } = clockState(bookings, today);
    const close = list => list.map(b => (b.id === open.id ? { ...b, end: time } : b));
    switch (action) {
      case 'in':
        if (state !== 'out') throw new Error('Es läuft bereits eine Buchung.');
        return [...bookings, makeBooking('work', today, time)];
      case 'break':
        if (state !== 'working') throw new Error('Pause nur aus laufender Arbeitszeit möglich.');
        return [...close(bookings), makeBooking('break', today, time)];
      case 'resume':
        if (state !== 'break') throw new Error('Keine laufende Pause.');
        return [...close(bookings), makeBooking('work', today, time)];
      case 'out':
        if (state === 'out') throw new Error('Keine laufende Buchung.');
        return close(bookings);
      default:
        throw new Error('Unbekannte Aktion: ' + action);
    }
  }

  // ---------- Import / Export ----------

  function csvEscape(v) {
    const s = v == null ? '' : String(v);
    return /[";\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
  }

  // CSV mit Semikolon (Excel-DE), eine Zeile je Buchung.
  function toCSV(bookings, ctx = {}) {
    const head = ['Datum', 'von', 'bis', 'Auftrag', 'Tätigkeit', 'Zeitart', 'Dauer', 'Status', 'Bemerkung'];
    const rows = sortBookings(bookings).map(b => {
      const min = bookingMinutes(b, ctx);
      return [toGermanDate(b.date), b.start, b.end || 'offen', b.order, b.activity, b.kind,
        min == null ? '' : formatDuration(min), b.end ? 'Abgeschlossen' : 'Offen', b.note || ''];
    });
    return [head, ...rows].map(r => r.map(csvEscape).join(';')).join('\r\n');
  }

  // Tagesübersicht als CSV (entspricht dem ersten Blatt der Google-Tabelle).
  function daysToCSV(days) {
    const head = ['Datum', 'Wochentag', 'KW', 'Einstempelzeit', 'Ausstempelzeit', 'Pausen', 'Arbeitszeit (netto)',
      'Gesamtdauer (inkl. Pausen)', 'Soll', 'Überstunden', 'Hinweise'];
    const rows = [...days].sort((a, b) => (a.date < b.date ? -1 : 1)).map(d => [
      toGermanDate(d.date), weekdayShort(d.date), d.week ? d.week.week : '', d.firstIn || '', d.lastOut || 'offen',
      formatDuration(d.breakMin), formatDuration(d.workMin), formatDuration(d.grossMin),
      formatDuration(d.target), formatDuration(d.overtimeMin, { signed: true }), d.hints.join(' | ')]);
    return [head, ...rows].map(r => r.map(csvEscape).join(';')).join('\r\n');
  }

  // Einfacher CSV-Parser (Semikolon oder Komma, Anführungszeichen).
  function parseCSV(text) {
    const rows = [];
    let row = [], field = '', inQ = false;
    const sep = (text.split('\n')[0] || '').includes(';') ? ';' : ',';
    for (let i = 0; i < text.length; i++) {
      const c = text[i];
      if (inQ) {
        if (c === '"' && text[i + 1] === '"') { field += '"'; i++; }
        else if (c === '"') inQ = false;
        else field += c;
      } else if (c === '"') inQ = true;
      else if (c === sep) { row.push(field); field = ''; }
      else if (c === '\n' || c === '\r') {
        if (c === '\r' && text[i + 1] === '\n') i++;
        row.push(field); rows.push(row); row = []; field = '';
      } else field += c;
    }
    if (field !== '' || row.length) { row.push(field); rows.push(row); }
    return rows.filter(r => r.some(x => x.trim() !== ''));
  }

  // Import aus dem CSV-Format von toCSV (oder dem Google-Sheet-Export).
  function fromCSV(text) {
    const rows = parseCSV(text);
    if (!rows.length) return { bookings: [], errors: ['Keine Daten gefunden.'] };
    const head = rows[0].map(h => h.trim().toLowerCase());
    const col = name => head.findIndex(h => h === name);
    const iDate = col('datum') >= 0 ? col('datum') : col('tag');
    const iFrom = col('von'), iTo = col('bis'), iAct = col('tätigkeit'), iOrder = col('auftrag');
    const iKind = col('zeitart'), iNote = col('bemerkung');
    const errors = [], bookings = [];
    if (iDate < 0 || iFrom < 0) return { bookings, errors: ['Spalten "Datum" und "von" werden benötigt.'] };
    rows.slice(1).forEach((r, n) => {
      const date = fromGermanDate(r[iDate]) || (parseISODate(r[iDate]) ? r[iDate].trim() : null);
      const start = normalizeHM(r[iFrom]);
      const rawEnd = (r[iTo] || '').trim().toLowerCase();
      const end = rawEnd === '' || rawEnd === 'offen' ? null : normalizeHM(rawEnd);
      const act = (r[iAct] || '').trim();
      const kindRaw = (iKind >= 0 ? r[iKind] : '').trim();
      const isBreak = /pause/i.test(kindRaw) || /pause/i.test(act) || (r[iOrder] || '').trim() === ACTIVITIES.break.order;
      const type = isBreak ? 'break' : 'work';
      if (!date || start == null) { errors.push('Zeile ' + (n + 2) + ': Datum oder Startzeit unlesbar.'); return; }
      if (rawEnd && rawEnd !== 'offen' && end == null) { errors.push('Zeile ' + (n + 2) + ': Endzeit unlesbar.'); return; }
      bookings.push(makeBooking(type, date, start, end, iNote >= 0 ? (r[iNote] || '').trim() : ''));
    });
    return { bookings: sortBookings(bookings), errors };
  }

  function normalizeHM(str) {
    const m = /^(\d{1,2}):(\d{2})(?::\d{2})?$/.exec(String(str || '').trim());
    if (!m) return null;
    return pad(Number(m[1])) + ':' + m[2];
  }

  return {
    KIND_WORK, KIND_BREAK, ACTIVITIES, DEFAULT_SETTINGS,
    parseHM, formatHM, formatDuration, normalizeHM,
    parseISODate, toISODate, todayISO, nowHM, fromGermanDate, toGermanDate, weekdayShort,
    isoWeekday, isoWeek, weekStart, addDays,
    newId, makeBooking, bookingMinutes, validateBooking, sortBookings,
    legalHints, dailyTarget, computeDay, computeDays, computeWeeks, computeTotals,
    clockState, punch,
    toCSV, daysToCSV, parseCSV, fromCSV,
  };
});
