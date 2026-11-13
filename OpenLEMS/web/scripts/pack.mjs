#!/usr/bin/env node
/**
 * 打包前端产物到 `application/manifest/web.zip`（Go 可执行文件会把它 embed 进去）。
 *
 * 为什么需要这个脚本：zip 不再入库，构建镜像时（`script/Dockerfile.dev` 的 web 阶段）
 * 就地构建它，Go 侧 `//go:embed` 只认这个位置。
 *
 * 为什么要自己写 zip（而不是调 `zip` 或 python3）：
 *   - 本机与 CI 都没有 `zip` 命令
 *   - 一开始用的是 python3 的 zipfile，结果在 `node:22-alpine` 里直接
 *     `spawnSync python3 ENOENT` —— 一个 Node 项目的打包脚本不该依赖另一种语言运行时
 *   - Node 的 zlib 自带 crc32 与 deflateRaw，写标准 zip 结构只要几十行（见下）
 *
 * 冒烟不去检查 zip 本身 —— 它检查 **dist**（`npm run verify` 里刚构建出来的那份）：
 * 页面 chunk 数必须等于导航页数 + 非导航页数。dist 对得上，pack 出来的 zip 自然对得上。
 */
import { execSync } from 'node:child_process'
import { mkdirSync, readFileSync, readdirSync, renameSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative, resolve } from 'node:path'
import { crc32, deflateRawSync } from 'node:zlib'

const webDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const distDir = join(webDir, 'dist')

// 默认写到 Go 侧 embed 的位置；Dockerfile 会用 --out 指到构建阶段的暂存目录
const outArg = process.argv.indexOf('--out')
const target =
  outArg > -1 && process.argv[outArg + 1]
    ? resolve(process.argv[outArg + 1])
    : resolve(webDir, '../application/manifest/web.zip')

/** 递归收集文件（相对路径 + 内容），路径统一用 / 分隔 */
function collect(dir, prefix = '') {
  const out = []
  for (const e of readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    const full = join(dir, e.name)
    const name = prefix ? `${prefix}/${e.name}` : e.name
    if (e.isDirectory()) out.push(...collect(full, name))
    else out.push({ name, data: readFileSync(full) })
  }
  return out
}

const DOS_DATE_1980_01_01 = 0x21 // 年份基数 1980、月 1、日 1
const DOS_TIME_MIDNIGHT = 0

/**
 * 最小 zip 写入器：deflate 压缩 + crc32，结构与 `zip`/python zipfile 产出的一致。
 * 固定成 1980-01-01 的时间戳是刻意的 —— 同样的输入产生逐字节相同的 zip，
 * 便于比对，也避免每次打包都因为时间戳变化而产生无意义的二进制差异。
 */
function buildZip(files) {
  const local = []
  const central = []
  let offset = 0

  for (const f of files) {
    const nameBuf = Buffer.from(f.name, 'utf8')
    const data = f.data
    const crc = crc32(data)
    const comp = deflateRawSync(data)

    const lh = Buffer.alloc(30)
    lh.writeUInt32LE(0x04034b50, 0) // 本地文件头签名
    lh.writeUInt16LE(20, 4) // 需要版本 2.0
    lh.writeUInt16LE(0, 6) // 通用标志位
    lh.writeUInt16LE(8, 8) // 压缩方法 8 = deflate
    lh.writeUInt16LE(DOS_TIME_MIDNIGHT, 10)
    lh.writeUInt16LE(DOS_DATE_1980_01_01, 12)
    lh.writeUInt32LE(crc, 14)
    lh.writeUInt32LE(comp.length, 18)
    lh.writeUInt32LE(data.length, 22)
    lh.writeUInt16LE(nameBuf.length, 26)
    lh.writeUInt16LE(0, 28) // 扩展字段长度
    local.push(lh, nameBuf, comp)

    const cd = Buffer.alloc(46)
    cd.writeUInt32LE(0x02014b50, 0) // 中央目录头签名
    cd.writeUInt16LE(20, 4) // 生成版本
    cd.writeUInt16LE(20, 6) // 需要版本
    cd.writeUInt16LE(0, 8)
    cd.writeUInt16LE(8, 10)
    cd.writeUInt16LE(DOS_TIME_MIDNIGHT, 12)
    cd.writeUInt16LE(DOS_DATE_1980_01_01, 14)
    cd.writeUInt32LE(crc, 16)
    cd.writeUInt32LE(comp.length, 20)
    cd.writeUInt32LE(data.length, 24)
    cd.writeUInt16LE(nameBuf.length, 28)
    cd.writeUInt16LE(0, 30) // 扩展字段
    cd.writeUInt16LE(0, 32) // 注释
    cd.writeUInt16LE(0, 34) // 起始磁盘号
    cd.writeUInt16LE(0, 36) // 内部属性
    cd.writeUInt32LE(0, 38) // 外部属性
    cd.writeUInt32LE(offset, 42) // 本地头偏移
    central.push(cd, nameBuf)

    offset += lh.length + nameBuf.length + comp.length
  }

  const cdBuf = Buffer.concat(central)
  const eocd = Buffer.alloc(22)
  eocd.writeUInt32LE(0x06054b50, 0) // 中央目录结束记录
  eocd.writeUInt16LE(0, 4)
  eocd.writeUInt16LE(0, 6)
  eocd.writeUInt16LE(files.length, 8)
  eocd.writeUInt16LE(files.length, 10)
  eocd.writeUInt32LE(cdBuf.length, 12)
  eocd.writeUInt32LE(offset, 16)
  eocd.writeUInt16LE(0, 20)
  return Buffer.concat([...local, cdBuf, eocd])
}

// 停用但按规范保留 .vue 文件的页面：不登记 loader、不产 chunk。
//
// 名单只允许「已停用」的页面进；名单外的 View 文件必须登记到 pages/index.ts 或
// router/routes.ts，否则数量比对会红 —— 「加了页面忘了登记」正是这个守卫要守的。
// smoke.mjs 的 PAUSED_WITHOUT_VIEW 是同一件事，改这里要同步改那里。
const PAUSED_WITHOUT_VIEW = new Set(['InferenceView'])

function countSourcePages() {
  const pagesDir = join(webDir, 'src/pages')
  let n = 0
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      if (e.isDirectory()) walk(join(dir, e.name))
      else if (e.name.endsWith('View.vue') && !PAUSED_WITHOUT_VIEW.has(e.name.slice(0, -'.vue'.length))) n++
    }
  }
  walk(pagesDir)
  return n
}

console.log('1/3 构建 dist…')
execSync('npm run build', { cwd: webDir, stdio: 'inherit' })

console.log('2/3 打包 web.zip…')
const files = collect(distDir)
const zip = buildZip(files)
mkdirSync(dirname(target), { recursive: true })
// 先写临时文件再改名：中途失败不会留下半个 zip
const tmpZip = `${target}.tmp`
writeFileSync(tmpZip, zip)
renameSync(tmpZip, target)

const names = files.map((f) => f.name)
const pageChunks = names
  .filter((f) => f.startsWith('assets/') && f.endsWith('.js') && /View-/.test(f))
  .map((f) => f.split('/').pop().replace(/-[A-Za-z0-9_-]+\.js$/, ''))

console.log('3/3 自检…')
console.log(`  web.zip: ${names.length} 个文件 / ${(zip.length / 1024).toFixed(0)} KB → ${relative(webDir, target)}`)
console.log(`  页面组件 chunk: ${pageChunks.length} 个`)

const srcPages = countSourcePages()
if (pageChunks.length !== srcPages) {
  console.error(`  ✗ 页面 chunk（${pageChunks.length}）与 src/pages 下的 *View.vue（${srcPages}）对不上`)
  console.error('    有页面没被引用（没登记到 pages/index.ts），或构建产物不完整。')
  process.exit(1)
}
if (!names.includes('index.html')) {
  console.error('  ✗ 包里没有 index.html —— 后端按根路径提供静态资源，缺它整个界面打不开')
  process.exit(1)
}
console.log('  ✓ 通过')
