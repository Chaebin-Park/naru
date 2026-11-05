import { lstat, readdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { routes, validateRoutes } from '../src/routes.mjs';

try {
  const [destination, requestID, ...extra] = process.argv.slice(2);
  if (!destination || requestID?.length !== 32 || !/^[a-f0-9]{32}$/.test(requestID) || extra.length !== 0
      || process.versions.node !== '22.22.2') {
    throw new Error('Invalid export arguments or runtime.');
  }
  const info = await lstat(destination);
  if (!info.isDirectory() || info.isSymbolicLink() || (await readdir(destination)).length !== 0) {
    throw new Error('Export destination must be a fresh empty directory.');
  }
  const envelope = {
    schema: 1,
    sample: 'naru-web',
    request_id: requestID,
    tools: { node: process.versions.node },
    routes: validateRoutes(routes),
  };
  const data = `${JSON.stringify(envelope)}\n`;
  if (Buffer.byteLength(data) > 1024 * 1024) {
    throw new Error('Export is too large.');
  }
  await writeFile(join(destination, 'web-export.json'), data, { flag: 'wx', mode: 0o600 });
} catch {
  // Do not echo source content, environment values or filesystem paths.
  process.stderr.write('Web export failed. Check the registry and fresh output directory.\n');
  process.exitCode = 1;
}
