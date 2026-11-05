import { createServer } from 'node:http';
import { routes, validateRoutes } from './routes.mjs';

// Importing this module never opens a port. Callers own listen and close.
export function createApp() {
  validateRoutes(routes);
  const handlers = new Map(routes.map(({ method, path, handler }) => [`${method}:${path}`, handler]));
  return createServer((request, response) => {
    // Registry paths are literal. Do not normalize encoded or dot segments into routes.
    const path = (request.url ?? '').split('?', 1)[0];
    const handler = handlers.get(`${request.method}:${path}`);
    response.setHeader('Content-Type', 'text/plain; charset=utf-8');
    if (!handler) {
      response.writeHead(404);
      response.end('not found');
      return;
    }
    try {
      const body = handler();
      if (typeof body !== 'string') {
        throw new Error('Invalid handler response.');
      }
      response.writeHead(200);
      response.end(body);
    } catch {
      response.writeHead(500);
      response.end('handler failed');
    }
  });
}
