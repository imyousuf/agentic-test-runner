import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Relative asset paths so the binary can serve the app from any mount point.
export default defineConfig({
  plugins: [react()],
  base: './',
  build: { outDir: 'dist', emptyOutDir: true },
});
