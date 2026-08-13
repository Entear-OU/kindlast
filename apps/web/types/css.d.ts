// Plain (non-module) stylesheet imports, e.g. `import './globals.css'` in the
// root layout.
//
// Next declares `*.module.css` in next/types/global.d.ts but not plain `*.css`,
// so a side-effect stylesheet import has no declaration to resolve against.
// TypeScript only complains once side-effect imports are checked
// (`noUncheckedSideEffectImports`, on by default from TypeScript 6), which is
// how this surfaced: CI resolved a newer compiler than the pinned one and
// failed with TS2882 on a file that had type-checked cleanly for months.
// Declaring it here makes the app correct under either setting.
declare module '*.css'
