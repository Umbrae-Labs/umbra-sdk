import { defineConfig } from 'tsdown'

export default defineConfig({
  entry: {
    index: 'src/index.ts',
    node: 'src/node.ts',
    electron: 'src/electron.ts',
  },
  format: ['esm', 'cjs'],
  dts: true,
  clean: true,
  sourcemap: true,
  target: 'es2022',
  outExtensions({ format }) {
    return {
      js: format === 'cjs' ? '.cjs' : '.js',
    }
  },
})
