// Bun 兼容性修复：process.stderr.fd 在 Bun 中为 undefined
// Vite 内部的 debug 模块会调用 tty.isatty(process.stderr.fd)
if (typeof process !== 'undefined' && process.stderr && process.stderr.fd === undefined) {
  process.stderr.fd = 2;
}

import { build } from 'vite';

build();
