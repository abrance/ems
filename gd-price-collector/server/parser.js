import * as cheerio from "cheerio";

const UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36";

const PAGE_LABELS = [
  "市场概况与日前申报",
  "日前成交与高峰节点电价",
  "低谷节点电价与价格曲线",
  "实时交易电价",
];

export function assertWechatUrl(raw) {
  let parsed;
  try {
    parsed = new URL(String(raw || "").trim());
  } catch {
    throw new Error("请粘贴完整的微信文章链接");
  }
  if (parsed.hostname !== "mp.weixin.qq.com") {
    throw new Error("只支持 mp.weixin.qq.com 公众号文章");
  }
  return parsed.toString();
}

export async function fetchAndParse(rawUrl) {
  const sourceUrl = assertWechatUrl(rawUrl);
  const html = await fetchHtml(sourceUrl);
  return parseArticle(html, sourceUrl);
}

export function parseArticle(html, sourceUrl) {
  const $ = cheerio.load(html);
  const title =
    attr($, 'meta[property="og:title"]', "content") ||
    text($, "#activity-name") ||
    pickJsString(html, "msg_title") ||
    text($, "title");

  if (!title) {
    throw new Error("未能读取文章标题，请确认链接可公开访问");
  }

  const dateInfo = parseDateFromTitle(title);
  if (!dateInfo) {
    throw new Error(`标题未包含运行日报日期：${title}`);
  }

  const author =
    attr($, 'meta[name="author"]', "content") ||
    text($, "#js_name") ||
    "广东电力交易中心";

  const images = selectReportImages(extractImages($, html));
  if (!images.length) {
    throw new Error("文章中没有识别到日报图片");
  }

  return {
    title,
    author,
    sourceUrl,
    year: dateInfo.year,
    month: dateInfo.month,
    day: dateInfo.day,
    date: dateInfo.date,
    images: images.map((img, index) => ({
      ...img,
      page: index + 1,
      label: PAGE_LABELS[index] || `日报第${index + 1}页`,
    })),
  };
}

export function parseDateFromTitle(title) {
  const yearMonth = title.match(/(\d{4})\s*年\s*(\d{1,2})\s*月/);
  const md = title.match(/[（(]\s*(\d{1,2})\s*[.．]\s*(\d{1,2})\s*[）)]/);
  if (!yearMonth || !md) return null;

  const year = Number(yearMonth[1]);
  const month = Number(yearMonth[2]);
  const day = Number(md[2]);
  if (!year || month < 1 || month > 12 || day < 1 || day > 31) return null;

  return {
    year,
    month,
    day,
    date: `${year}-${pad(month)}-${pad(day)}`,
  };
}

async function fetchHtml(url) {
  const res = await fetch(url, {
    headers: {
      "User-Agent": UA,
      Accept: "text/html,application/xhtml+xml",
      "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
      Referer: "https://mp.weixin.qq.com/",
    },
  });
  if (!res.ok) {
    throw new Error(`拉取文章失败：HTTP ${res.status}`);
  }
  return res.text();
}

function extractImages($, html) {
  const fromDom = [];
  $("#js_content img").each((_, el) => {
    const src = $(el).attr("data-src") || $(el).attr("src") || "";
    if (!/mmbiz\.qpic\.cn/.test(src)) return;
    const width = Number($(el).attr("data-w") || 0);
    const ratio = Number($(el).attr("data-ratio") || 0);
    fromDom.push({
      originUrl: normalizeImageUrl(src),
      width,
      height: width && ratio ? Math.round(width * ratio) : 0,
    });
  });

  const fromList = parsePictureList(html);
  if (!fromDom.length) return fromList;
  if (!fromList.length) return fromDom;

  return fromDom.map((img, index) => {
    const hit =
      fromList.find((item) => sameImage(item.originUrl, img.originUrl)) ||
      fromList[index];
    if (!hit) return img;
    return {
      ...img,
      width: img.width || hit.width,
      height: img.height || hit.height,
    };
  });
}

function parsePictureList(html) {
  const match = html.match(/picturePageInfoList\s*=\s*"([\s\S]*?)"\s*;/);
  if (!match) return [];

  const raw = match[1]
    .replace(/\\x26amp;/g, "&")
    .replace(/&amp;/g, "&")
    .replace(/\\"/g, '"')
    .replace(/'/g, '"')
    .replace(/,(\s*])/g, "$1");

  let list;
  try {
    list = JSON.parse(raw);
  } catch {
    return [];
  }

  return list
    .map((item) => ({
      originUrl: normalizeImageUrl(item.cdn_url || ""),
      width: Number(item.width || 0),
      height: Number(item.height || 0),
    }))
    .filter((item) => item.originUrl);
}

function selectReportImages(images) {
  const sized = images.filter((img) => img.width && img.height);
  if (sized.length === images.length && images.length) {
    const reports = images.filter(
      (img) => img.height > img.width && img.width >= 1500
    );
    if (reports.length) return reports;
  }
  if (images.length >= 3) {
    return images.slice(1, -1);
  }
  return images;
}

export function normalizeImageUrl(url) {
  return String(url || "")
    .replace(/\\x26amp;/g, "&")
    .replace(/&amp;/g, "&")
    .replace(/\/640\?/, "/0?")
    .replace(/^http:\/\//, "https://");
}

function sameImage(a, b) {
  return stripQuery(a) === stripQuery(b);
}

function stripQuery(url) {
  return url.split("?")[0].replace(/\/0$/, "/").replace(/\/640$/, "/");
}

function attr($, selector, name) {
  return ($(selector).attr(name) || "").trim();
}

function text($, selector) {
  return $(selector).first().text().replace(/\s+/g, " ").trim();
}

function pickJsString(html, name) {
  const match = html.match(new RegExp(`var\\s+${name}\\s*=\\s*"([\\s\\S]*?)"\\s*;`));
  if (!match) return "";
  return match[1].replace(/\\"/g, '"').replace(/&amp;/g, "&").trim();
}

function pad(n) {
  return String(n).padStart(2, "0");
}
