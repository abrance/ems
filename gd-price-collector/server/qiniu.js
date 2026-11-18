import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import qiniu from "qiniu";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const UPLOAD_ROOT = path.join(__dirname, "../data/uploads");
const WECHAT_HOST = process.env.WECHAT_HOST || "mp.weixin.qq.com";
const UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36";

export function isQiniuConfigured() {
  return Boolean(
    process.env.QINIU_ACCESS_KEY &&
      process.env.QINIU_SECRET_KEY &&
      process.env.QINIU_BUCKET &&
      process.env.QINIU_DOMAIN
  );
}

export async function persistImages(date, images) {
  const stored = [];
  for (const image of images) {
    const buffer = await downloadImage(image.originUrl);
    const ext = guessExt(image.originUrl, buffer);
    const key = `gd-spot/${date}/page-${image.page}${ext}`;
    const saved = await saveObject(key, buffer);
    stored.push({
      page: image.page,
      label: image.label,
      width: image.width,
      height: image.height,
      key,
      url: saved.url,
      storage: saved.storage,
    });
  }
  return stored;
}

async function saveObject(key, buffer) {
  if (isQiniuConfigured()) {
    try {
      return await uploadToQiniu(key, buffer);
    } catch (err) {
      console.error("[qiniu] upload failed, fallback to local:", err.message);
    }
  }
  return saveLocal(key, buffer);
}

async function uploadToQiniu(key, buffer) {
  const accessKey = process.env.QINIU_ACCESS_KEY;
  const secretKey = process.env.QINIU_SECRET_KEY;
  const bucket = process.env.QINIU_BUCKET;
  const domain = String(process.env.QINIU_DOMAIN).replace(/\/$/, "");

  const mac = new qiniu.auth.digest.Mac(accessKey, secretKey);
  const putPolicy = new qiniu.rs.PutPolicy({
    scope: `${bucket}:${key}`,
    expires: 3600,
  });
  const uploadToken = putPolicy.uploadToken(mac);

  const config = new qiniu.conf.Config();
  applyZone(config, process.env.QINIU_ZONE);

  const formUploader = new qiniu.form_up.FormUploader(config);
  const putExtra = new qiniu.form_up.PutExtra();
  putExtra.mimeType = "image/png";

  const { data, resp } = await formUploader.put(uploadToken, key, buffer, putExtra);
  if (resp.statusCode !== 200) {
    throw new Error(`七牛上传失败：HTTP ${resp.statusCode} ${JSON.stringify(data)}`);
  }

  return {
    storage: "qiniu",
    url: `${domain}/${key}`,
  };
}

async function saveLocal(key, buffer) {
  const abs = path.join(UPLOAD_ROOT, key);
  await fs.mkdir(path.dirname(abs), { recursive: true });
  await fs.writeFile(abs, buffer);
  return {
    storage: "local",
    url: `/uploads/${key}`,
  };
}

async function downloadImage(url) {
  const res = await fetch(url, {
    headers: {
      "User-Agent": UA,
      Referer: `https://${WECHAT_HOST}/`,
      Accept: "image/avif,image/webp,image/apng,image/*,*/*;q=0.8",
    },
  });
  if (!res.ok) {
    throw new Error(`下载图片失败：HTTP ${res.status}`);
  }
  return Buffer.from(await res.arrayBuffer());
}

function applyZone(config, zone) {
  const map = {
    z0: qiniu.zone.Zone_z0,
    z1: qiniu.zone.Zone_z1,
    z2: qiniu.zone.Zone_z2,
    na0: qiniu.zone.Zone_na0,
    as0: qiniu.zone.Zone_as0,
  };
  if (zone && map[zone]) {
    config.zone = map[zone];
  }
}

function guessExt(url, buffer) {
  if (url.includes("wx_fmt=jpeg") || url.includes("wx_fmt=jpg")) return ".jpg";
  if (url.includes("wx_fmt=gif")) return ".gif";
  if (buffer[0] === 0xff && buffer[1] === 0xd8) return ".jpg";
  return ".png";
}
