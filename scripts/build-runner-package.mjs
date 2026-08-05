import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const root = resolve(import.meta.dirname, '..');
const dist = resolve(root, 'dist');
const staging = resolve(dist, 'runner-package');
const downloads = resolve(dist, 'downloads');
const project = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
const packageName = `projectboard-runner-${project.version}.tgz`;

rmSync(staging, { recursive: true, force: true });
mkdirSync(resolve(staging, 'runner'), { recursive: true });
mkdirSync(resolve(staging, 'platform'), { recursive: true });
mkdirSync(downloads, { recursive: true });

cpSync(resolve(dist, 'runner'), resolve(staging, 'runner'), { recursive: true });
cpSync(resolve(root, 'src/runner/tray.ps1'), resolve(staging, 'platform/projectboard-runner-tray.ps1'));
cpSync(resolve(root, 'deploy/projectboard-runner.service'), resolve(staging, 'platform/projectboard-runner.service'));

writeFileSync(resolve(staging, 'package.json'), JSON.stringify({
  name: '@projectboard/runner',
  version: project.version,
  description: 'Trusted local execution runner for ProjectBoard',
  type: 'module',
  bin: { 'projectboard-runner': 'runner/main.js' },
  engines: { node: '>=24' },
  files: ['runner', 'platform', 'README.md'],
}, null, 2));
writeFileSync(resolve(staging, 'README.md'), `# ProjectBoard Runner\n\nRequires Node.js 24+ and an authenticated Codex CLI.\n\nInstall the downloaded package with:\n\n\`\`\`sh\nnpm install --global ./${packageName}\n\`\`\`\n\nCreate an Agent in ProjectBoard, then run the one-time pairing command shown in the web interface.\n`);

rmSync(resolve(downloads, packageName), { force: true });
const npmCommand = process.platform === 'win32' ? process.execPath : 'npm';
const npmArgs = process.platform === 'win32'
  ? [resolve(dirname(process.execPath), 'node_modules/npm/bin/npm-cli.js'), 'pack', staging, '--pack-destination', downloads]
  : ['pack', staging, '--pack-destination', downloads];
const packed = spawnSync(npmCommand, npmArgs, {
  cwd: root,
  stdio: 'inherit',
});
if (packed.error) console.error(packed.error);
if (packed.status !== 0) process.exit(packed.status ?? 1);

cpSync(resolve(root, 'src/runner/tray.ps1'), resolve(downloads, 'projectboard-runner-tray.ps1'));
cpSync(resolve(root, 'deploy/projectboard-runner.service'), resolve(downloads, 'projectboard-runner.service'));
