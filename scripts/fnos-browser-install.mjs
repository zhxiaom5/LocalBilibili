#!/usr/bin/env node
import { createRequire } from 'node:module';
import { execFileSync } from 'node:child_process';
import { createInterface } from 'node:readline/promises';
import { stdin as input, stdout as output } from 'node:process';
import { fileURLToPath } from 'node:url';
import * as fsSync from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';

const require = createRequire(import.meta.url);
const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

function parseArgs(argv) {
  const args = {
    login: false,
    install: false,
    dryRun: false,
    selfTestSelectors: false,
    headed: false,
    fpk: '',
    fnosUrl: process.env.FNOS_WEB_URL || 'http://192.168.50.247:5666',
    verifyUrl: process.env.LOCALBILIBILI_VERIFY_URL || process.env.FNOS_VERIFY_URL || '',
    state: process.env.FNOS_STORAGE_STATE || path.join(rootDir, '.local', 'fnos', 'storage-state.json'),
    timeoutMs: Number(process.env.FNOS_DEPLOY_TIMEOUT_MS || 120000),
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    switch (arg) {
      case '--login':
        args.login = true;
        break;
      case '--install':
        args.install = true;
        break;
      case '--dry-run':
        args.dryRun = true;
        break;
      case '--self-test-selectors':
        args.selfTestSelectors = true;
        break;
      case '--headed':
        args.headed = true;
        break;
      case '--fpk':
        args.fpk = path.resolve(argv[++i]);
        break;
      case '--fnos-url':
        args.fnosUrl = argv[++i];
        break;
      case '--verify-url':
        args.verifyUrl = argv[++i];
        break;
      case '--state':
        args.state = path.resolve(argv[++i]);
        break;
      default:
        throw new Error(`unknown argument: ${arg}`);
    }
  }

  if (!args.login && !args.install && !args.selfTestSelectors) {
    throw new Error('choose --login or --install');
  }
  if (!args.fpk && (args.install || args.selfTestSelectors)) {
    args.fpk = selectHighestVersionedFpk(path.join(rootDir, 'dist'));
  }
  return args;
}

function parseVersion(value) {
  return String(value || '')
    .split('.')
    .map((part) => Number(part))
    .filter((part) => Number.isInteger(part) && part >= 0);
}

function compareVersions(left, right) {
  const a = parseVersion(left);
  const b = parseVersion(right);
  const length = Math.max(a.length, b.length);
  for (let i = 0; i < length; i += 1) {
    const diff = (a[i] || 0) - (b[i] || 0);
    if (diff !== 0) {
      return diff;
    }
  }
  return 0;
}

function selectHighestVersionedFpk(distDir) {
  let entries = [];
  try {
    entries = fsSync.readdirSync(distDir, { withFileTypes: true });
  } catch {
    throw new Error(`No versioned LocalBilibili FPK found under ${distDir}. Run with --build or pass --fpk PATH.`);
  }

  const candidates = [];
  for (const entry of entries) {
    if (!entry.isFile()) {
      continue;
    }
    const match = entry.name.match(/^localbilibili-v(\d+(?:\.\d+)*)(-root)?-x86_64\.fpk$/);
    if (!match) {
      continue;
    }
    candidates.push({
      file: path.join(distDir, entry.name),
      version: match[1],
      rootRank: match[2] ? -1 : 0,
    });
  }

  if (candidates.length === 0) {
    throw new Error(`No versioned LocalBilibili FPK found under ${distDir}. Run with --build or pass --fpk PATH.`);
  }
  candidates.sort((left, right) => {
    const versionDiff = compareVersions(left.version, right.version);
    if (versionDiff !== 0) {
      return versionDiff;
    }
    return left.rootRank - right.rootRank;
  });
  return candidates[candidates.length - 1].file;
}

function readFpkVersion(fpkPath) {
  let manifest = '';
  try {
    manifest = execFileSync('tar', ['-xOf', fpkPath, 'manifest'], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    });
  } catch (error) {
    throw new Error(`could not read manifest from FPK: ${fpkPath}`);
  }
  const version = manifest.match(/^version\s*=\s*(.+)$/m)?.[1]?.trim();
  if (!version) {
    throw new Error(`version field was not found in FPK manifest: ${fpkPath}`);
  }
  return version;
}

function loadPlaywright() {
  try {
    const resolved = require.resolve('playwright', { paths: [path.join(rootDir, 'web')] });
    const playwright = require(resolved);
    if (typeof playwright?.chromium?.launch !== 'function') {
      throw new Error('Playwright runtime loaded, but chromium.launch is unavailable');
    }
    return playwright;
  } catch (error) {
    if (error.message?.includes('chromium.launch')) {
      throw error;
    }
    throw new Error('Playwright is not installed. Run: npm install --prefix web');
  }
}

function log(message) {
  console.error(`[fnos-playwright] ${message}`);
}

async function pathExists(file) {
  try {
    await fs.access(file);
    return true;
  } catch {
    return false;
  }
}

function visibleText(value) {
  return String(value || '').replace(/\s+/g, ' ').trim();
}

function textLabels(envName, fallback) {
  return [process.env[envName], fallback].filter(Boolean);
}

async function saveFailureArtifact(page, name) {
  const outDir = path.join(rootDir, 'output', 'fnos-deploy');
  await fs.mkdir(outDir, { recursive: true });
  const stamp = new Date().toISOString().replace(/[:.]/g, '-');
  const screenshot = path.join(outDir, `${stamp}-${name}.png`);
  await page.screenshot({ path: screenshot, fullPage: true });
  log(`saved failure screenshot: ${screenshot}`);
}

async function waitForManualLogin(page, statePath) {
  log(`opened fnOS: ${page.url()}`);
  log('finish login in the visible browser, then return here and press Enter.');
  const rl = createInterface({ input, output });
  await rl.question('Press Enter after fnOS login is complete...');
  rl.close();
  await fs.mkdir(path.dirname(statePath), { recursive: true });
  await page.context().storageState({ path: statePath });
  log(`saved storageState: ${statePath}`);
}

async function looksLoggedOut(page) {
  const passwordFields = await page.locator('input[type="password"]').count();
  if (passwordFields > 0) {
    return true;
  }
  const loginText = page.getByText(/登录|登入|Sign in|Login/i).first();
  return loginText.isVisible({ timeout: 1500 }).catch(() => false);
}

async function openFnos(page, fnosUrl, timeoutMs) {
  await page.goto(fnosUrl, { waitUntil: 'domcontentloaded', timeout: timeoutMs });
  await page.waitForLoadState('networkidle', { timeout: Math.min(timeoutMs, 30000) }).catch(() => {});
}

async function logStep(page, name) {
  log(`step ok: ${name}`);
  if (process.env.FNOS_DEPLOY_STEP_SCREENSHOTS !== '1') {
    return;
  }
  const outDir = path.join(rootDir, 'output', 'fnos-deploy');
  await fs.mkdir(outDir, { recursive: true });
  const stamp = new Date().toISOString().replace(/[:.]/g, '-');
  const screenshot = path.join(outDir, `${stamp}-${name}.png`);
  await page.screenshot({ path: screenshot, fullPage: true });
}

function scopedGetByText(scope, label) {
  return typeof label === 'string'
    ? scope.getByText(label, { exact: false })
    : scope.getByText(label);
}

async function clickFirstVisible(scope, labels, timeoutMs) {
  for (const label of labels) {
    const locator = scopedGetByText(scope, label);
    const count = await locator.count().catch(() => 0);
    for (let i = 0; i < count; i += 1) {
      const candidate = locator.nth(i);
      if (await candidate.isVisible().catch(() => false)) {
        await candidate.click({ timeout: Math.min(timeoutMs, 30000) });
        return true;
      }
    }
  }
  return false;
}

async function hasVisibleText(scope, labels) {
  for (const label of labels) {
    const locator = scopedGetByText(scope, label);
    const count = await locator.count().catch(() => 0);
    for (let i = 0; i < count; i += 1) {
      if (await locator.nth(i).isVisible().catch(() => false)) {
        return true;
      }
    }
  }
  return false;
}

async function waitForVisibleText(page, labels, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await hasVisibleText(page, labels)) {
      return true;
    }
    await page.waitForTimeout(500);
  }
  return false;
}

async function currentDialog(page, titlePattern) {
  const dialog = await findDialog(page, titlePattern);
  if (dialog) {
    return dialog;
  }
  return page.locator('body');
}

async function findDialog(page, titlePattern) {
  const candidates = page.locator([
    '[role="dialog"]',
    '.semi-portal',
    '.semi-modal-wrap',
    '.semi-modal-confirm',
    '.semi-modal-confirm-content',
    '.modal',
    '.dialog',
    '.ant-modal',
    '.el-dialog',
    '.arco-modal',
    '.semi-modal',
    '.n-modal',
    '.adm-modal',
    '.f-modal',
  ].join(','));
  const count = await candidates.count().catch(() => 0);
  for (let i = count - 1; i >= 0; i -= 1) {
    const candidate = candidates.nth(i);
    if (!(await candidate.isVisible().catch(() => false))) {
      continue;
    }
    const text = await candidate.innerText({ timeout: 1000 }).catch(() => '');
    if (!titlePattern || titlePattern.test(visibleText(text))) {
      return promoteDialogContainer(candidate);
    }
  }
  return null;
}

function promoteDialogContainer(locator) {
  return locator.locator(
    'xpath=ancestor-or-self::*[contains(concat(" ", normalize-space(@class), " "), " semi-modal-confirm ") or contains(concat(" ", normalize-space(@class), " "), " semi-modal-wrap ") or contains(concat(" ", normalize-space(@class), " "), " semi-portal ") or @role="dialog" or contains(concat(" ", normalize-space(@class), " "), " modal ")][1]',
  );
}

async function assertNoInstallError(page, timeoutMs) {
  const errorText = page.getByText(/安装失败|升级失败|更新失败|上传失败|校验失败|未知网络错误|网络错误|错误|失败|Failed|Error/i).first();
  if (await errorText.isVisible({ timeout: Math.min(timeoutMs, 2000) }).catch(() => false)) {
    const text = await errorText.innerText().catch(() => 'fnOS reported an error');
    throw new Error(`fnOS reported an error: ${visibleText(text)}`);
  }
}

async function openAppCenter(page, timeoutMs) {
  const manualInstallLabels = textLabels('FNOS_MANUAL_INSTALL_TEXT', /^手动安装$/i);
  if (await hasVisibleText(page, manualInstallLabels)) {
    await logStep(page, 'app-center-ready');
    return;
  }

  const appCenterLabels = textLabels('FNOS_APP_CENTER_TEXT', /^应用中心$|App Center/i);
  if (await clickFirstVisible(page, appCenterLabels, timeoutMs)) {
    await page.waitForLoadState('networkidle', { timeout: 30000 }).catch(() => {});
    if (await waitForVisibleText(page, manualInstallLabels, Math.min(timeoutMs, 60000))) {
      await logStep(page, 'app-center-opened-by-icon');
      return;
    }
  }

  const homeUrl = new URL('/', page.url()).href;
  await page.goto(homeUrl, { waitUntil: 'domcontentloaded', timeout: timeoutMs });
  await page.waitForLoadState('networkidle', { timeout: 30000 }).catch(() => {});
  await page.waitForTimeout(1000);

  if (await clickFirstVisible(page, appCenterLabels, timeoutMs)) {
    await page.waitForLoadState('networkidle', { timeout: 30000 }).catch(() => {});
    if (await waitForVisibleText(page, manualInstallLabels, Math.min(timeoutMs, 60000))) {
      await logStep(page, 'app-center-opened-by-home-retry');
      return;
    }
  }

  if (!(await hasVisibleText(page, manualInstallLabels))) {
    throw new Error('could not open fnOS app center or find manual install entry');
  }
  await logStep(page, 'app-center-opened-after-home-retry');
}

async function clickManualInstall(page, timeoutMs) {
  const installLabels = textLabels('FNOS_MANUAL_INSTALL_TEXT', /^手动安装$/i);

  if (await clickFirstVisible(page, installLabels, timeoutMs)) {
    await page.waitForTimeout(800);
    await logStep(page, 'manual-install-opened');
    return;
  }

  throw new Error('could not find fnOS manual install entry');
}

async function chooseUploadFromComputer(page, fpkPath, timeoutMs) {
  const dialog = await currentDialog(page, /手动安装|Manual install/i);
  const fileInputs = dialog.locator('input[type="file"]');
  const uploadLabels = textLabels(
    'FNOS_UPLOAD_FROM_COMPUTER_TEXT',
    /从电脑上传|选择文件|上传|浏览|选择安装包|选择 FPK|Choose file|Upload|Browse/i,
  );

  for (const label of uploadLabels) {
    const locator = scopedGetByText(dialog, label);
    const count = await locator.count().catch(() => 0);
    for (let i = 0; i < count; i += 1) {
      const candidate = locator.nth(i);
      if (!(await candidate.isVisible().catch(() => false))) {
        continue;
      }
      const chooserPromise = page.waitForEvent('filechooser', { timeout: 10000 }).catch(() => null);
      await candidate.click({ timeout: Math.min(timeoutMs, 30000) });
      const chooser = await chooserPromise;
      if (chooser) {
        await chooser.setFiles(fpkPath);
        await waitForUploadTransition(page, timeoutMs);
        await logStep(page, 'fpk-uploaded-by-filechooser');
        return;
      }
      if ((await fileInputs.count()) > 0) {
        await fileInputs.first().setInputFiles(fpkPath);
        await waitForUploadTransition(page, timeoutMs);
        await logStep(page, 'fpk-uploaded-after-click');
        return;
      }
    }
  }

  if ((await fileInputs.count()) > 0) {
    await fileInputs.first().setInputFiles(fpkPath);
    await waitForUploadTransition(page, timeoutMs);
    await logStep(page, 'fpk-uploaded-by-input');
    return;
  }

  throw new Error('could not find fnOS FPK upload control');
}

async function waitForUploadTransition(page, timeoutMs) {
  const uploadTimeoutMs = Number(process.env.FNOS_UPLOAD_TIMEOUT_MS || timeoutMs);
  const deadline = Date.now() + uploadTimeoutMs;
  await page.waitForTimeout(800);
  while (Date.now() < deadline) {
    const uploadVisible = await page.getByText(/上传中|正在上传|Uploading/i).first().isVisible({ timeout: 500 }).catch(() => false);
    const nextVisible = await page.getByText(/未经验证应用的安全提示|未经飞牛验证|单击同意|安全提示|未验证|更新\s*LocalBilibili|检查设置|更新完成后立即启用|更新中|安装中|升级中|正在更新|正在安装|Updating|Installing/i).first().isVisible({ timeout: 500 }).catch(() => false);
    if (!uploadVisible || nextVisible) {
      await page.waitForTimeout(500);
      return;
    }
    await page.waitForTimeout(1000);
  }
  throw new Error('fnOS FPK upload did not finish before timeout');
}

async function acceptSecurityWarning(page, timeoutMs) {
  await page.waitForTimeout(800);
  const dialog = await findDialog(page, /未经验证应用的安全提示|未经飞牛验证|单击同意|安全提示|未验证|unknown publisher|security/i);
  if (!dialog) {
    const warningVisible = await page.getByText(/未经验证应用的安全提示|未经飞牛验证|单击同意|安全提示|未验证|unknown publisher|security/i).first().isVisible({ timeout: 1500 }).catch(() => false);
    if (!warningVisible) {
      log('security warning dialog was not shown; continuing');
      return;
    }
    const pageAgreeButton = page.getByRole('button', { name: /^同意$|Agree|Accept/i }).first();
    if (await pageAgreeButton.isVisible({ timeout: 1500 }).catch(() => false)) {
      await pageAgreeButton.click({ timeout: Math.min(timeoutMs, 30000) });
      await page.waitForTimeout(800);
      await logStep(page, 'security-warning-accepted');
      return;
    }
    await saveFailureArtifact(page, 'security-warning-failed').catch(() => {});
    throw new Error('fnOS security warning was visible, but agree button was not found');
  }
  const warningVisible = await dialog.getByText(/未经验证应用的安全提示|未经飞牛验证|单击同意|安全提示|未验证|unknown publisher|security/i).first().isVisible({ timeout: 1000 }).catch(() => false);
  if (!warningVisible) {
    log('security warning dialog was not shown; continuing');
    return;
  }
  const labels = textLabels('FNOS_SECURITY_AGREE_TEXT', /^同意$|Agree|Accept/i);
  if (!(await clickFirstVisible(dialog, labels, timeoutMs))) {
    await saveFailureArtifact(page, 'security-warning-failed').catch(() => {});
    throw new Error('could not find fnOS security warning agree button');
  }
  await page.waitForTimeout(800);
  await logStep(page, 'security-warning-accepted');
}

async function acceptLicenseAgreement(page, timeoutMs) {
  await page.waitForTimeout(800);
  const dialog = await findDialog(page, /协议许可|许可协议|License|Agreement/i);
  const scope = dialog || page.locator('body');
  const licenseVisible = await scope.getByText(/协议许可|许可协议|我接受许可协议|License|Agreement/i).first().isVisible({ timeout: 1500 }).catch(() => false);
  if (!licenseVisible) {
    log('license agreement dialog was not shown; continuing');
    return;
  }

  const checkbox = scope.locator('input[type="checkbox"]').first();
  if ((await checkbox.count().catch(() => 0)) > 0) {
    if (!(await checkbox.isChecked().catch(() => false))) {
      await checkbox.check({ timeout: Math.min(timeoutMs, 30000), force: true }).catch(async () => {
        await scope.getByText(/我接受许可协议|接受许可|I accept/i).first().click({ timeout: Math.min(timeoutMs, 30000) });
      });
    }
  } else if (!(await clickFirstVisible(scope, [/我接受许可协议|接受许可|I accept/i], timeoutMs))) {
    await saveFailureArtifact(page, 'license-agreement-failed').catch(() => {});
    throw new Error('could not find fnOS license agreement checkbox');
  }

  if (!(await clickFirstVisible(scope, [/^下一步$|^继续$|^确定$|Next|Continue|OK/i], timeoutMs))) {
    await saveFailureArtifact(page, 'license-next-failed').catch(() => {});
    throw new Error('could not find fnOS license agreement next button');
  }
  await page.waitForTimeout(1000);
  await logStep(page, 'license-agreement-accepted');
}

async function completeInstallSettings(page, timeoutMs) {
  await page.waitForTimeout(800);
  const dialog = await findDialog(page, /LocalBilibili\s*设置|应用端口|App port|Settings/i);
  const scope = dialog || page.locator('body');
  const settingsVisible = await scope.getByText(/LocalBilibili\s*设置|应用端口|App port/i).first().isVisible({ timeout: 1500 }).catch(() => false);
  if (!settingsVisible) {
    log('install settings dialog was not shown; continuing');
    return;
  }

  const portInput = scope.locator('input').first();
  if ((await portInput.count().catch(() => 0)) > 0) {
    await portInput.fill(process.env.FNOS_LOCALBILIBILI_PORT || '8090', { timeout: Math.min(timeoutMs, 30000) });
  }

  if (!(await clickFirstVisible(scope, [/^下一步$|^继续$|^确定$|Next|Continue|OK/i], timeoutMs))) {
    await saveFailureArtifact(page, 'install-settings-next-failed').catch(() => {});
    throw new Error('could not find fnOS install settings next button');
  }
  await page.waitForTimeout(1200);
  await logStep(page, 'install-settings-confirmed');
}

async function confirmUpdateCheck(page, timeoutMs) {
  const alreadyCurrent = await findDialog(page, /无法安装\s*LocalBilibili|已安装相同或更高版本|same or higher version/i);
  if (alreadyCurrent) {
    const closed = await clickFirstVisible(alreadyCurrent, [/^知道了$|^确定$|OK|Got it/i], timeoutMs);
    if (!closed) {
      const acknowledge = page.getByRole('button', { name: /^知道了$|^确定$|OK|Got it/i }).first();
      if (await acknowledge.isVisible({ timeout: 1000 }).catch(() => false)) {
        await acknowledge.click({ timeout: Math.min(timeoutMs, 30000) });
      }
    }
    await page.waitForTimeout(800);
    const stillVisible = await page.getByText(/无法安装\s*LocalBilibili|已安装相同或更高版本/i).first().isVisible({ timeout: 1000 }).catch(() => false);
    if (stillVisible) {
      const fallbackButton = page.getByText(/^知道了$|^确定$/).first();
      await fallbackButton.click({ timeout: Math.min(timeoutMs, 30000) }).catch(() => {});
      await page.waitForTimeout(800);
    }
    await logStep(page, 'already-current-version');
    return 'already-current';
  }

  const dialog = await findDialog(page, /更新\s*LocalBilibili|检查设置|更新完成后立即启用|确认安装|确认升级|Update\s*LocalBilibili/i);
  if (!dialog) {
    const updatingVisible = await page.getByText(/更新中|安装中|升级中|正在更新|正在安装|Updating|Installing/i).first().isVisible({ timeout: 2000 }).catch(() => false);
    if (updatingVisible) {
      await logStep(page, 'update-started-without-confirm');
      return 'confirmed';
    }
    log('update confirmation dialog was not shown; continuing to installed version verification');
    return 'confirmed';
  }
  const enableAfterUpdate = dialog.getByText(/更新完成后立即启用/).first();
  if (await enableAfterUpdate.isVisible({ timeout: 1000 }).catch(() => false)) {
    const checkbox = dialog.locator('input[type="checkbox"]').first();
    if ((await checkbox.count().catch(() => 0)) > 0 && !(await checkbox.isChecked().catch(() => true))) {
      await checkbox.check().catch(() => {});
    }
  }
  const labels = textLabels('FNOS_UPDATE_CONFIRM_TEXT', /^确定$|^确认$|OK|Confirm/i);
  if (!(await clickFirstVisible(dialog, labels, timeoutMs))) {
    await saveFailureArtifact(page, 'update-confirm-failed').catch(() => {});
    throw new Error('could not find fnOS update confirmation button');
  }
  await page.waitForTimeout(1200);
  await logStep(page, 'update-check-confirmed');
  return 'confirmed';
}

async function waitForUpgradeComplete(page, timeoutMs) {
  const success = page.getByText(/安装成功|升级成功|已安装|运行中|打开|启动|Installed|Updated|Running|Open/i).first();
  const failure = page.getByText(/安装失败|升级失败|错误|失败|Failed|Error/i).first();

  const result = await Promise.race([
    success.waitFor({ state: 'visible', timeout: timeoutMs }).then(() => 'success').catch(() => null),
    failure.waitFor({ state: 'visible', timeout: timeoutMs }).then(() => 'failure').catch(() => null),
  ]);

  if (result === 'failure') {
    throw new Error('fnOS reported install failure');
  }
  if (result !== 'success') {
    await page.waitForTimeout(5000);
    await assertNoInstallError(page, timeoutMs);
    log('install success text was not detected; no visible error found, continuing');
    return;
  }
  await logStep(page, 'upgrade-completed');
}

function parseInstalledVersionText(text) {
  return visibleText(text).match(/当前版本\s*([0-9]+(?:\.[0-9]+)+)/)?.[1] || '';
}

function assertInstalledVersionMatches(text, expectedVersion) {
  const installedVersion = parseInstalledVersionText(text);
  if (!installedVersion) {
    throw new Error('could not find LocalBilibili current version in fnOS app center');
  }
  if (installedVersion !== expectedVersion) {
    throw new Error(`LocalBilibili installed version ${installedVersion} does not match FPK version ${expectedVersion}`);
  }
  return installedVersion;
}

async function openInstalledLocalBilibiliDetail(page, timeoutMs) {
  const alreadyCurrentVisible = await page.getByText(/无法安装\s*LocalBilibili|已安装相同或更高版本/i).first().isVisible({ timeout: 1000 }).catch(() => false);
  if (alreadyCurrentVisible) {
    await clickFirstVisible(page, [/^知道了$|^确定$|OK|Got it/i], timeoutMs).catch(() => false);
    await page.waitForTimeout(800);
  }

  const bodyText = await page.locator('body').innerText({ timeout: 3000 }).catch(() => '');
  if (parseInstalledVersionText(bodyText)) {
    return;
  }

  await clickFirstVisible(page, [/^已安装$/i], timeoutMs).catch(() => false);
  await page.waitForTimeout(800);
  const localBilibiliEntries = page.getByText(/^LocalBilibili$/);
  const entryCount = await localBilibiliEntries.count().catch(() => 0);
  if (entryCount > 0) {
    await localBilibiliEntries.nth(entryCount - 1).click({ timeout: Math.min(timeoutMs, 30000) }).catch(() => {});
  } else {
    await clickFirstVisible(page, [/LocalBilibili/i], timeoutMs).catch(() => false);
  }
  await page.waitForTimeout(1200);
}

async function verifyInstalledAppVersion(page, expectedVersion, timeoutMs) {
  await openInstalledLocalBilibiliDetail(page, timeoutMs);
  const bodyText = await page.locator('body').innerText({ timeout: 10000 }).catch(() => '');
  try {
    const installedVersion = assertInstalledVersionMatches(bodyText, expectedVersion);
    log(`verified installed LocalBilibili version: ${installedVersion}`);
  } catch (error) {
    await saveFailureArtifact(page, 'version-mismatch-failed').catch(() => {});
    throw error;
  }
}

async function verifyLocalBilibili(context, verifyUrl, timeoutMs) {
  if (!verifyUrl) {
    return;
  }
  const page = await context.newPage();
  try {
    const response = await page.goto(verifyUrl, { waitUntil: 'domcontentloaded', timeout: timeoutMs });
    const status = response?.status() ?? 0;
    const title = await page.title().catch(() => '');
    const html = await page.content().catch(() => '');
    const bodyText = await page.locator('body').innerText({ timeout: 10000 }).catch(() => '');
    if (status >= 400 || !`${title}\n${bodyText}\n${html}`.includes('LocalBilibili')) {
      log(`health check warning: LocalBilibili text was not found at ${verifyUrl}`);
      return;
    }
    log(`health check ok: ${verifyUrl}`);
  } catch (error) {
    log(`health check warning: ${error.message}`);
  } finally {
    await page.close().catch(() => {});
  }
}

async function loginFlow(args, chromium) {
  const browser = await chromium.launch({ headless: false });
  const context = await browser.newContext({ ignoreHTTPSErrors: true });
  const page = await context.newPage();
  try {
    await openFnos(page, args.fnosUrl, args.timeoutMs);
    await waitForManualLogin(page, args.state);
  } finally {
    await browser.close();
  }
}

function mockFnosHtml(installedVersion = '0.1.12') {
  return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <title>fnOS mock</title>
  <style>
    body { font-family: sans-serif; }
    .modal, .app-center, .semi-portal { display: none; }
    .modal, .semi-modal-confirm { border: 1px solid #ddd; padding: 24px; margin: 16px; background: white; }
    .app-center { padding: 16px; }
    button { margin: 8px; padding: 8px 16px; }
  </style>
</head>
<body>
  <button id="desktop-store">应用中心</button>
  <button id="background-confirm">确定</button>
  <p>grafana.loki</p>
  <section class="app-center" id="app-center">
    <h1>应用中心</h1>
    <button id="manual-install">手动安装</button>
  </section>
  <section class="modal" role="dialog" id="manual-modal">
    <h2>手动安装</h2>
    <p>如需要手动安装应用，请上传 fpk 文件</p>
    <button id="upload-button">从电脑上传</button>
    <input id="fpk-input" type="file" hidden>
  </section>
  <div class="semi-portal" id="security-modal">
    <div role="none" class="semi-modal-wrap semi-modal-wrap-center">
      <div class="semi-modal-confirm">
        <div class="semi-modal-confirm-content semi-modal-confirm-content-withIcon">
          <h2>未经验证应用的安全提示</h2>
          <p>LocalBilibili 由 未知发布者 提供，未经飞牛验证。单击同意，即表示你同意全权负责。</p>
        </div>
        <div class="semi-modal-confirm-btns">
          <button>取消</button>
          <button id="agree-button">同意</button>
        </div>
      </div>
    </div>
  </div>
  <div class="semi-portal" id="update-modal">
    <div role="none" class="semi-modal-wrap semi-modal-wrap-center">
      <div class="semi-modal-confirm">
        <div class="semi-modal-confirm-content">
          <h2>更新 LocalBilibili - 检查设置</h2>
          <label><input checked type="checkbox"> 更新完成后立即启用</label>
        </div>
        <div class="semi-modal-confirm-btns">
          <button>取消</button>
          <button id="confirm-button">确定</button>
        </div>
      </div>
    </div>
  </div>
  <div class="semi-portal" id="license-modal">
    <div role="none" class="semi-modal-wrap semi-modal-wrap-center">
      <div class="semi-modal-confirm">
        <div class="semi-modal-confirm-content">
          <h2>安装 LocalBilibili - 协议许可</h2>
          <p>阅读许可协议，然后再继续进行。</p>
          <label><input id="license-checkbox" type="checkbox"> 我接受许可协议的条款</label>
        </div>
        <div class="semi-modal-confirm-btns">
          <button>取消</button>
          <button id="license-next" disabled>下一步</button>
        </div>
      </div>
    </div>
  </div>
  <div class="semi-portal" id="settings-modal">
    <div role="none" class="semi-modal-wrap semi-modal-wrap-center">
      <div class="semi-modal-confirm">
        <div class="semi-modal-confirm-content">
          <h2>安装 LocalBilibili - LocalBilibili 设置</h2>
          <label>应用端口 <input id="wizard-port" value="8090"></label>
        </div>
        <div class="semi-modal-confirm-btns">
          <button>取消</button>
          <button id="settings-next">下一步</button>
        </div>
      </div>
    </div>
  </div>
  <p id="result"></p>
  <section id="app-detail" style="display:none">
    <h2>LocalBilibili</h2>
    <dl>
      <dt>当前版本</dt>
      <dd>${installedVersion}</dd>
    </dl>
  </section>
  <script>
    const show = (id) => { document.getElementById(id).style.display = 'block'; };
    const hide = (id) => { document.getElementById(id).style.display = 'none'; };
    document.getElementById('desktop-store').onclick = () => show('app-center');
    document.getElementById('manual-install').onclick = () => show('manual-modal');
    document.getElementById('upload-button').onclick = () => document.getElementById('fpk-input').click();
    document.getElementById('fpk-input').onchange = () => { hide('manual-modal'); show('security-modal'); };
    document.getElementById('agree-button').onclick = () => { hide('security-modal'); show('license-modal'); };
    document.getElementById('license-checkbox').onchange = (event) => {
      document.getElementById('license-next').disabled = !event.target.checked;
    };
    document.getElementById('license-next').onclick = () => { hide('license-modal'); show('settings-modal'); };
    document.getElementById('settings-next').onclick = () => { hide('settings-modal'); show('update-modal'); };
    document.getElementById('confirm-button').onclick = () => {
      hide('update-modal');
      show('app-detail');
      document.getElementById('result').textContent = '已安装 打开 运行中';
    };
    document.getElementById('background-confirm').onclick = () => {
      document.getElementById('result').textContent = '误点背景';
    };
  </script>
</body>
</html>`;
}

async function selfTestSelectors(args, chromium) {
  const expectedVersion = readFpkVersion(args.fpk);
  const browser = await chromium.launch({ headless: !args.headed });
  const context = await browser.newContext({ ignoreHTTPSErrors: true, acceptDownloads: true });
  const page = await context.newPage();
  try {
    await page.setContent(mockFnosHtml(expectedVersion), { waitUntil: 'domcontentloaded' });
    await openAppCenter(page, args.timeoutMs);
    await clickManualInstall(page, args.timeoutMs);
    await chooseUploadFromComputer(page, args.fpk, args.timeoutMs);
    await acceptSecurityWarning(page, args.timeoutMs);
    await acceptLicenseAgreement(page, args.timeoutMs);
    await completeInstallSettings(page, args.timeoutMs);
    const confirmation = await confirmUpdateCheck(page, args.timeoutMs);
    if (confirmation !== 'already-current') {
      await waitForUpgradeComplete(page, args.timeoutMs);
    }
    await verifyInstalledAppVersion(page, expectedVersion, args.timeoutMs);
    const result = visibleText(await page.locator('#result').innerText());
    if (result.includes('误点背景')) {
      throw new Error('selector self-test clicked the background instead of the active dialog');
    }
    try {
      assertInstalledVersionMatches(`LocalBilibili 当前版本 0.0.0`, expectedVersion);
      throw new Error('selector self-test failed to detect version mismatch');
    } catch (error) {
      if (!error.message.includes('does not match FPK version')) {
        throw error;
      }
    }
    log('selector self-test completed');
  } finally {
    await browser.close();
  }
}

async function installFlow(args, chromium) {
  if (!(await pathExists(args.state))) {
    throw new Error(`storageState not found: ${args.state}. Run --login first.`);
  }
  if (!(await pathExists(args.fpk))) {
    throw new Error(`FPK not found: ${args.fpk}`);
  }
  const expectedVersion = readFpkVersion(args.fpk);
  log(`selected FPK: ${args.fpk}`);
  log(`expected LocalBilibili version: ${expectedVersion}`);

  const browser = await chromium.launch({ headless: !args.headed });
  const context = await browser.newContext({
    storageState: args.state,
    ignoreHTTPSErrors: true,
    acceptDownloads: true,
  });
  const page = await context.newPage();

  try {
    await openFnos(page, args.fnosUrl, args.timeoutMs);
    if (await looksLoggedOut(page)) {
      throw new Error('fnOS login state is expired. Run --login again.');
    }

    await openAppCenter(page, args.timeoutMs);
    if (args.dryRun) {
      log(`dry-run ok: fnOS app center and manual install entry are reachable. FPK: ${args.fpk}; version: ${expectedVersion}`);
      return;
    }

    await clickManualInstall(page, args.timeoutMs);
    await chooseUploadFromComputer(page, args.fpk, args.timeoutMs);
    await acceptSecurityWarning(page, args.timeoutMs);
    await acceptLicenseAgreement(page, args.timeoutMs);
    await completeInstallSettings(page, args.timeoutMs);
    await confirmUpdateCheck(page, args.timeoutMs);
    await waitForUpgradeComplete(page, args.timeoutMs);
    await verifyInstalledAppVersion(page, expectedVersion, args.timeoutMs);
    await verifyLocalBilibili(context, args.verifyUrl, args.timeoutMs);
    log('fnOS install/upgrade flow completed');
  } catch (error) {
    await saveFailureArtifact(page, 'install-failed').catch(() => {});
    throw error;
  } finally {
    await browser.close();
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const { chromium } = loadPlaywright();

  if (args.login) {
    await loginFlow(args, chromium);
  }
  if (args.selfTestSelectors) {
    await selfTestSelectors(args, chromium);
  }
  if (args.install) {
    await installFlow(args, chromium);
  }
}

main().catch((error) => {
  console.error(`[fnos-playwright] ${error.message}`);
  process.exit(1);
});
