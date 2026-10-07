#!/usr/bin/env node
/**
 * 打包前端产物到 `application/manifest/web.zip`（Go 可执行文件会把它 embed 进去）。
 *
 * 为什么需要这个脚本：web.zip 是**部署时真正跑的东西**，而 dist/ 被 gitignore 了 ——
 * 也就是说「源码改了但没打包」时，部署出去的是旧界面，而且没有任何东西会提醒你。
 * 之前就是这个状态：源码已 31 页，web.zip 里只有 16 个页面组件。
 *
 * 冒烟不去检查 zip 本身 —— 它检查 **dist**（`npm run verify` 里刚构建出来的那份）：
 * 页面 chunk 数必须等于导航页数 + 非导航页数。dist 对得上，pack 出来的 zip 自然对得上，
 * 而 zip 不再进仓库，也就不存在「提交的产物过期」这一类问题。
 *
 * 打包用 python3 的 zipfile：本机与 CI 都没有 `zip` 命令，而 zipfile 是标准库。
 */
import { execFileSync, execSync } from 'node:child_process'
import { mkdirSync, readdirSync, renameSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative, resolve } from 'node:path'

const webDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const distDir = join(webDir, 'dist')

// 默认写到 Go 侧 embed 的位置；Dockerfile 会用 --out 指到构建阶段的暂存目录
const outArg = process.argv.indexOf('--out')
const target =
  outArg > -1 && process.argv[outArg + 1]
    ? resolve(process.argv[outArg + 1])
    : resolve(webDir, '../application/manifest/web.zip')

const PY_ZIP = `
import os, sys, zipfile
src, dst = sys.argv[1], sys.argv[2]
names = []
with zipfile.ZipFile(dst, "w", zipfile.ZIP_DEFLATED) as z:
    for root, _dirs, files in os.walk(src):
        for f in sorted(files):
            full = os.path.join(root, f)
            rel = os.path.relpath(full, src)
            z.write(full, rel)
            names.append(rel)
print("\\n".join(sorted(names)))
`

function countSourcePages() {
  const pagesDir = join(webDir, 'src/pages')
  let n = 0
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      if (e.isDirectory()) walk(join(dir, e.name))
      else if (e.name.endsWith('View.vue')) n++
    }
  }
  walk(pagesDir)
  return n
}

console.log('1/3 构建 dist…')
execSync('npm run build', { cwd: webDir, stdio: 'inherit' })

console.log('2/3 打包 web.zip…')
mkdirSync(dirname(target), { recursive: true })
// 临时文件放在**目标同目录**：跨文件系统的 rename 会 EXDEV 失败（/tmp 与仓库常常不在一块盘上）
const tmpZip = `${target}.tmp`
const listed = execFileSync('python3', ['-c', PY_ZIP, distDir, tmpZip], { encoding: 'utf-8' })
  .split('\n')
  .map((l) => l.trim())
  .filter(Boolean)
renameSync(tmpZip, target) // 先写临时文件再替换：中途失败不会留下半个 zip

const files = listed
const pageChunks = files
  .filter((f) => f.startsWith('assets/') && f.endsWith('.js') && /View-/.test(f))
  .map((f) => f.split('/').pop().replace(/-[A-Za-z0-9_-]+\.js$/, ''))

console.log('3/3 自检…')
const sizeKb = (statSync(target).size / 1024).toFixed(0)
console.log(`  web.zip: ${files.length} 个文件 / ${sizeKb} KB → ${relative(webDir, target)}`)
console.log(`  页面组件 chunk: ${pageChunks.length} 个`)

const srcPages = countSourcePages()
if (pageChunks.length !== srcPages) {
  console.error(`  ✗ 页面 chunk（${pageChunks.length}）与 src/pages 下的 *View.vue（${srcPages}）对不上`)
  console.error('    有页面没被引用（没登记到 pages/index.ts），或构建产物不完整。')
  process.exit(1)
}
if (!files.includes('index.html')) {
  console.error('  ✗ 包里没有 index.html —— 后端按根路径提供静态资源，缺它整个界面打不开')
  process.exit(1)
}
console.log('  ✓ 通过')
