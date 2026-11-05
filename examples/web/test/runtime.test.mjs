import assert from 'node:assert/strict';
import { request } from 'node:http';
import { once } from 'node:events';
import test from 'node:test';
import { message } from 'naru-web-message';
import { routes, validateRoutes } from '../src/routes.mjs';
import { createApp } from '../src/server.mjs';

function fetchRoute(port, method, path) {
  return new Promise((resolve, reject) => {
    const req = request({ hostname: '127.0.0.1', port, method, path, agent: false }, (res) => {
      let body = '';
      res.setEncoding('utf8');
      res.on('data', (chunk) => { body += chunk; });
      res.on('end', () => resolve({ status: res.statusCode, body }));
      res.on('error', reject);
    });
    const deadline = setTimeout(() => req.destroy(new Error('Request deadline exceeded.')), 2000);
    req.on('close', () => clearTimeout(deadline));
    req.on('error', reject);
    req.end();
  });
}

test('registered routes match actual HTTP dispatch and local workspace behavior', { timeout: 8000 }, async () => {
  assert.equal(message(), 'Hello from the local workspace.');
  const expected = new Map([
    ['GET:/health', 'ok'],
    ['GET:/message', 'Hello from the local workspace.'],
  ]);
  const server = createApp();
  try {
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    const port = server.address().port;
    for (const { method, path } of validateRoutes(routes)) {
      const key = `${method}:${path}`;
      assert.ok(expected.has(key), 'Add an independent response expectation for a new route.');
      assert.deepEqual(await fetchRoute(port, method, path), { status: 200, body: expected.get(key) });
    }
    assert.deepEqual(await fetchRoute(port, 'GET', '/missing'), { status: 404, body: 'not found' });
    for (const path of ['/x/../health', '/%68ealth']) {
      assert.equal((await fetchRoute(port, 'GET', path)).status, 404);
    }
    for (const path of ['/health', '/message']) {
      if (!routes.some((route) => route.method === 'GET' && route.path === path)) {
        assert.equal((await fetchRoute(port, 'GET', path)).status, 404);
      }
    }
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close((error) => {
      if (error && error.code !== 'ERR_SERVER_NOT_RUNNING') reject(error);
      else resolve();
    }));
  }
});

test('registry rejects ambiguous routes and invalid handlers without quoting input', () => {
  const valid = { method: 'GET', path: '/health', handler: () => 'ok' };
  assert.deepEqual(validateRoutes([]), []);
  assert.throws(() => validateRoutes([valid, valid]), /Duplicate/);
  for (const path of ['/a/:id', '/a?x=1', '/a#fragment', '/a/', '//a', '/a/..', '/%61', '/health\n', 'SENTINEL']) {
    assert.throws(() => validateRoutes([{ ...valid, path }]), (error) => !error.message.includes(path));
  }
  assert.throws(() => validateRoutes([{ ...valid, method: 'get' }]));
  assert.throws(() => validateRoutes([{ ...valid, handler: 'not a function' }]));
  assert.throws(() => validateRoutes([{ ...valid, extra: true }]));
});
