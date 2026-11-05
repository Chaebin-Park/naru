import { message } from 'naru-web-message';

export const routes = [
  { method: 'GET', path: '/health', handler: () => 'ok' },
  { method: 'GET', path: '/message', handler: () => message() },
];

// The runtime and build use this same bounded registry contract.
export function validateRoutes(registry) {
  if (!Array.isArray(registry) || registry.length > 1024) {
    throw new Error('Invalid route registry.');
  }
  const methods = new Set(['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS', 'HEAD']);
  const seen = new Set();
  const descriptions = registry.map((route) => {
    if (route === null || typeof route !== 'object' || Array.isArray(route)
        || Object.keys(route).sort().join(',') !== 'handler,method,path'
        || !methods.has(route.method) || typeof route.path !== 'string'
        || /[\x00-\x20\x7f]/.test(route.path) || typeof route.handler !== 'function') {
      throw new Error('Invalid route entry.');
    }
    const segments = route.path.split('/').slice(1);
    if (route.path !== '/' && (!/^\/[A-Za-z0-9._-]+(?:\/[A-Za-z0-9._-]+)*$/.test(route.path)
        || segments.some((part) => part === '.' || part === '..'))) {
      throw new Error('Unsupported route path.');
    }
    const id = `${route.method}:${route.path}`;
    if (id.length > 128 || seen.has(id)) {
      throw new Error('Duplicate or oversized route identity.');
    }
    seen.add(id);
    return { method: route.method, path: route.path };
  });
  return descriptions.sort((a, b) => {
    const left = `${a.method}:${a.path}`;
    const right = `${b.method}:${b.path}`;
    return left < right ? -1 : left > right ? 1 : 0;
  });
}
