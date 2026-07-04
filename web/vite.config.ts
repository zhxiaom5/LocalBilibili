import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    globals: true,
    testTimeout: 12000,
    fileParallelism: false,
    exclude: ['node_modules/**', 'dist/**', 'e2e/**'],
  },
});
