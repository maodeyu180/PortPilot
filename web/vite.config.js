import { defineConfig } from 'vite';

export default defineConfig({
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        // Preserve the browser Host so the API's same-origin check also works
        // for JSON POST requests through the development proxy.
        changeOrigin: false,
      },
    },
  },
  build: { sourcemap: false },
});
