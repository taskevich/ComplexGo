import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const projectDir = fileURLToPath(new URL('.', import.meta.url));
const publicDir = fileURLToPath(new URL('./public', import.meta.url));

export default defineConfig(({ command }) => ({
  root: publicDir,
  envDir: projectDir,
  plugins: [react()],
  // Dev HTML must be transformed by Vite; the build also copies static files.
  publicDir: command === 'build' ? publicDir : false,
  resolve: {
    alias: {
      '/src': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: fileURLToPath(new URL('./dist', import.meta.url)),
    emptyOutDir: true,
  },
}));
