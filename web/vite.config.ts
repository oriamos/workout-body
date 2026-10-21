import { defineConfig, loadEnv, type ProxyOptions } from 'vite';
import react from '@vitejs/plugin-react';

// The browser calls same-origin /api/v1/...; this proxy forwards to the workout engine and adds the
// bearer token on the server side. The token comes from WORKOUT_API_TOKEN (never a VITE_* variable),
// so it is never bundled into client code or visible in the browser.
export default defineConfig(({ mode }) => {
  const env = { ...loadEnv(mode, process.cwd(), ''), ...process.env };
  const mock = env.VITE_DATA_SOURCE === 'mock';
  const target = env.WORKOUT_API_URL ?? 'http://127.0.0.1:8080';
  const token = env.WORKOUT_API_TOKEN;
  if (!mock && !token) {
    console.warn(
      '\n⚠  WORKOUT_API_TOKEN is not set: every /api request will be rejected (401).\n' +
        `   Set it to the engine's MCP_BEARER_TOKEN, e.g. WORKOUT_API_TOKEN=... npm run dev  (proxy target: ${target})\n` +
        '   Or run the in-memory prototype with: npm run dev:mock\n',
    );
  }
  const proxy: Record<string, ProxyOptions> = {
    '/api': {
      target,
      changeOrigin: true,
      configure(p) {
        p.on('proxyReq', (req) => {
          req.removeHeader('cookie'); // the engine needs none; don't forward the dev origin's cookies
          if (token) req.setHeader('Authorization', `Bearer ${token}`);
          else req.removeHeader('authorization');
        });
      },
    },
  };
  return { plugins: [react()], server: { port: 5173, proxy }, preview: { proxy } };
});
