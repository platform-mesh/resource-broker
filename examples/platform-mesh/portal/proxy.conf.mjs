/**
 * Proxy configuration for Angular dev server (Vite-based)
 *
 * This proxies /api requests to the Platform Mesh portal to avoid CORS issues
 * during local development.
 */
export default {
  '/api': {
    target: 'https://bob.portal.localhost:8443',
    secure: false,
    changeOrigin: true,
    configure: (proxy, options) => {
      proxy.on('proxyReq', (proxyReq, req, res) => {
        console.log('[Proxy] Forwarding:', req.method, req.url, '-> ', options.target + req.url);
      });
      proxy.on('proxyRes', (proxyRes, req, res) => {
        console.log('[Proxy] Response:', proxyRes.statusCode, req.url);
      });
      proxy.on('error', (err, req, res) => {
        console.error('[Proxy] Error:', err.message);
      });
    }
  }
};
