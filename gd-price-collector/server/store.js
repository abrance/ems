import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const DATA_FILE = path.join(__dirname, "../data/records.json");

export async function listRecords() {
  const records = await readAll();
  return records.map(summary);
}

export async function getRecord(date) {
  const records = await readAll();
  return records.find((item) => item.date === date) || null;
}

export async function saveRecord(record) {
  const records = await readAll();
  const next = {
    ...record,
    notes: record.notes || "",
    updatedAt: new Date().toISOString(),
  };
  const idx = records.findIndex((item) => item.date === next.date);
  if (idx >= 0) {
    next.notes = next.notes || records[idx].notes || "";
    records[idx] = next;
  } else {
    records.unshift(next);
  }
  records.sort((a, b) => b.date.localeCompare(a.date));
  await writeAll(records);
  return next;
}

export async function updateNotes(date, notes) {
  const record = await getRecord(date);
  if (!record) return null;
  record.notes = String(notes || "");
  return saveRecord(record);
}

async function readAll() {
  try {
    const raw = await fs.readFile(DATA_FILE, "utf8");
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch (err) {
    if (err.code === "ENOENT") return [];
    throw err;
  }
}

async function writeAll(records) {
  await fs.mkdir(path.dirname(DATA_FILE), { recursive: true });
  const tmp = `${DATA_FILE}.tmp`;
  await fs.writeFile(tmp, JSON.stringify(records, null, 2));
  await fs.rename(tmp, DATA_FILE);
}

function summary(record) {
  return {
    date: record.date,
    year: record.year,
    month: record.month,
    day: record.day,
    title: record.title,
    author: record.author,
    pageCount: (record.images || []).length,
    collectedAt: record.collectedAt,
    storage: (record.images || [])[0]?.storage || "local",
  };
}
