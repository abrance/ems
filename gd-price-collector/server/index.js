import path from "node:path";
import { fileURLToPath } from "node:url";
import dotenv from "dotenv";
import express from "express";
import { fetchAndParse } from "./parser.js";
import { persistImages, isQiniuConfigured } from "./qiniu.js";
import { getRecord, listRecords, saveRecord, updateNotes } from "./store.js";

dotenv.config();

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PORT = Number(process.env.PORT || 3001);

const app = express();
app.use(express.json({ limit: "1mb" }));
app.use("/uploads", express.static(path.join(__dirname, "../data/uploads")));

app.get("/api/health", (_req, res) => {
  res.json({ ok: true });
});

app.get("/api/config", (_req, res) => {
  res.json({
    qiniu: isQiniuConfigured(),
    storage: isQiniuConfigured() ? "qiniu" : "local",
  });
});

app.get("/api/records", async (_req, res, next) => {
  try {
    res.json({ list: await listRecords() });
  } catch (err) {
    next(err);
  }
});

app.get("/api/records/:date", async (req, res, next) => {
  try {
    const record = await getRecord(req.params.date);
    if (!record) {
      res.status(404).json({ error: "未找到该日档案" });
      return;
    }
    res.json(record);
  } catch (err) {
    next(err);
  }
});

app.post("/api/records/:date/notes", async (req, res, next) => {
  try {
    const record = await updateNotes(req.params.date, req.body?.notes);
    if (!record) {
      res.status(404).json({ error: "未找到该日档案" });
      return;
    }
    res.json(record);
  } catch (err) {
    next(err);
  }
});

app.post("/api/collect", async (req, res, next) => {
  try {
    const parsed = await fetchAndParse(req.body?.url);
    const images = await persistImages(parsed.date, parsed.images);
    const record = await saveRecord({
      ...parsed,
      images,
      collectedAt: new Date().toISOString(),
    });
    res.json(record);
  } catch (err) {
    next(err);
  }
});

app.use((err, _req, res, _next) => {
  console.error(err);
  res.status(400).json({ error: err.message || "采集失败" });
});

app.listen(PORT, "0.0.0.0", () => {
  console.log(`gd-price-collector api http://127.0.0.1:${PORT}`);
});
