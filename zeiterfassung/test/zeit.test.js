const test = require('node:test');
const assert = require('node:assert/strict');
const Z = require('../zeit.js');
const Seed = require('../seed-data.js');

test('parseHM / formatHM / formatDuration', () => {
  assert.equal(Z.parseHM('08:05'), 485);
  assert.equal(Z.parseHM('8:05'), 485);
  assert.equal(Z.parseHM('24:00'), null);
  assert.equal(Z.parseHM(''), null);
  assert.equal(Z.formatHM(485), '08:05');
  assert.equal(Z.formatDuration(450), '7:30');
  assert.equal(Z.formatDuration(-30, { signed: true }), '-0:30');
  assert.equal(Z.formatDuration(30, { signed: true }), '+0:30');
});

test('Datumsumwandlung und ISO-Kalenderwoche wie im Sheet', () => {
  assert.equal(Z.fromGermanDate('17.08.2026'), '2026-08-17');
  assert.equal(Z.toGermanDate('2026-08-17'), '17.08.2026');
  assert.equal(Z.isoWeek('2026-08-17').week, 34);
  assert.equal(Z.isoWeek('2026-08-24').week, 35);
  assert.equal(Z.isoWeek('2026-08-31').week, 36);
  assert.equal(Z.isoWeek('2026-09-07').week, 37);
  assert.equal(Z.isoWeek('2026-01-01').key, '2026-W01');
  assert.equal(Z.weekStart('2026-09-11'), '2026-09-07');
  assert.equal(Z.isoWeekday('2026-09-13'), 7);
});

test('Tagesauswertung 18.08.2026 aus den Seed-Daten', () => {
  const d = Z.computeDay('2026-08-18', Seed.bookings(), {}, { today: '2026-09-11' });
  assert.equal(d.firstIn, '08:01');
  assert.equal(d.lastOut, '16:53');
  assert.equal(d.breakMin, 12);
  assert.equal(d.workMin, 195 + 247 + 78);
  assert.equal(d.grossMin, 532);
  assert.equal(d.target, 480);
  assert.equal(d.overtimeMin, 520 - 480);
  assert.equal(d.open, 0);
  // > 6 h Arbeit mit nur 12 min Pause -> ArbZG-Hinweis
  assert.ok(d.hints.some(h => h.includes('30 min')));
});

test('offene Buchung an einem vergangenen Tag wird als nicht ausgestempelt markiert', () => {
  const d = Z.computeDay('2026-08-20', Seed.bookings(), {}, { today: '2026-09-11', nowMinutes: 600 });
  assert.equal(d.open, 1);
  assert.equal(d.openPast, true);
  assert.equal(d.lastOut, null);
  assert.ok(d.hints[0].includes('nicht ausgestempelt'));
  // offene Buchung zählt nicht in die Arbeitszeit
  assert.equal(d.workMin, 181 + 124 + 183);
});

test('offene Buchung heute läuft bis jetzt', () => {
  const b = [Z.makeBooking('work', '2026-09-11', '08:00')];
  const d = Z.computeDay('2026-09-11', b, {}, { today: '2026-09-11', nowMinutes: 10 * 60 + 30 });
  assert.equal(d.workMin, 150);
  assert.equal(d.openPast, false);
  assert.equal(d.running.id, b[0].id);
});

test('laufende Buchung mit Start nach der aktuellen Uhrzeit zählt 0 statt 24 h', () => {
  // Kommt vor, wenn die Uhr des Stempelterminals der Rechneruhr voraus ist.
  const b = [Z.makeBooking('work', '2026-09-11', '15:25')];
  const ctx = { today: '2026-09-11', nowMinutes: 14 * 60 + 19 };
  assert.equal(Z.bookingMinutes(b[0], ctx), 0);
  const d = Z.computeDay('2026-09-11', b, {}, ctx);
  assert.equal(d.workMin, 0);
  assert.deepEqual(d.hints, []);
  // Abgeschlossene Buchung über Mitternacht bleibt unberührt
  const nacht = Z.makeBooking('work', '2026-09-11', '22:00', '02:00');
  assert.equal(Z.bookingMinutes(nacht, ctx), 240);
});

test('Wochen- und Gesamtauswertung', () => {
  const days = Z.computeDays(Seed.bookings(), {}, { today: '2026-09-11' });
  assert.equal(days.length, 16);
  assert.equal(days[0].date, '2026-09-07');
  const weeks = Z.computeWeeks(days);
  assert.deepEqual(weeks.map(w => w.week), [37, 36, 35, 34]);
  const kw35 = weeks.find(w => w.week === 35);
  assert.equal(kw35.days.length, 5);
  assert.equal(kw35.start, '2026-08-24');
  assert.equal(kw35.end, '2026-08-30');
  assert.equal(kw35.target, 5 * 480);
  const totals = Z.computeTotals(days);
  assert.equal(totals.open, 2);
  assert.equal(totals.days, 16);
  assert.equal(totals.grossMin, totals.workMin + totals.breakMin);
});

test('Soll berücksichtigt Wochenstunden und Arbeitstage', () => {
  assert.equal(Z.dailyTarget({ weeklyHours: 40, workDays: [1, 2, 3, 4, 5] }), 480);
  assert.equal(Z.dailyTarget({ weeklyHours: 32, workDays: [1, 2, 3, 4] }), 480);
  assert.equal(Z.dailyTarget({ weeklyHours: 35, workDays: [1, 2, 3, 4, 5] }), 420);
  // Samstag ist kein Arbeitstag -> Soll 0, alles Überstunden
  const d = Z.computeDay('2026-09-12', [Z.makeBooking('work', '2026-09-12', '09:00', '11:00')]);
  assert.equal(d.target, 0);
  assert.equal(d.overtimeMin, 120);
});

test('Stempeluhr: Kommen -> Pause -> Weiter -> Gehen', () => {
  const today = '2026-09-11';
  let b = [];
  assert.equal(Z.clockState(b, today).state, 'out');
  b = Z.punch(b, 'in', today, '07:00');
  assert.equal(Z.clockState(b, today).state, 'working');
  assert.throws(() => Z.punch(b, 'in', today, '07:01'));
  b = Z.punch(b, 'break', today, '12:00');
  assert.equal(Z.clockState(b, today).state, 'break');
  assert.equal(b[0].end, '12:00');
  assert.equal(b[1].kind, 'Pause');
  b = Z.punch(b, 'resume', today, '12:30');
  assert.equal(Z.clockState(b, today).state, 'working');
  b = Z.punch(b, 'out', today, '16:00');
  assert.equal(Z.clockState(b, today).state, 'out');
  assert.equal(b.length, 3);
  const d = Z.computeDay(today, b);
  assert.equal(d.workMin, 8 * 60 + 30);
  assert.equal(d.breakMin, 30);
  assert.equal(d.hints.length, 0);
});

test('CSV-Export und Re-Import sind verlustfrei', () => {
  const src = Seed.bookings();
  const csv = Z.toCSV(src, { today: '2026-09-11' });
  assert.ok(csv.startsWith('Datum;von;bis;Auftrag'));
  const { bookings, errors } = Z.fromCSV(csv);
  assert.deepEqual(errors, []);
  assert.equal(bookings.length, src.length);
  const strip = l => l.map(b => [b.date, b.start, b.end, b.kind, b.note].join('|'));
  assert.deepEqual(strip(Z.sortBookings(bookings)), strip(Z.sortBookings(src)));
});

test('CSV-Import versteht das Google-Sheet-Format (Tag/von/bis/Auftrag/Tätigkeit)', () => {
  const csv = 'Tag;von;bis;Auftrag;Tätigkeit\n07.09.2026;06:54;offen;000999;Kommen\n04.09.2026;15:00;15:07;000007;*Pause*\n';
  const { bookings, errors } = Z.fromCSV(csv);
  assert.deepEqual(errors, []);
  assert.equal(bookings.length, 2);
  assert.equal(bookings[0].date, '2026-09-04');
  assert.equal(bookings[0].kind, 'Pause');
  assert.equal(bookings[1].end, null);
  assert.equal(bookings[1].kind, 'Arbeitszeit');
});

test('validateBooking meldet ungültige Felder', () => {
  assert.deepEqual(Z.validateBooking({ date: '2026-09-11', start: '08:00', end: null, kind: 'Arbeitszeit' }), []);
  const errs = Z.validateBooking({ date: '11.09.2026', start: '8', end: '25:00', kind: 'Urlaub' });
  assert.equal(errs.length, 4);
});
