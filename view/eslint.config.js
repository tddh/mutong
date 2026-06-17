import js from '@eslint/js'
import vue from 'eslint-plugin-vue'
import tseslint from '@typescript-eslint/eslint-plugin'
import tsparser from '@typescript-eslint/parser'
import eslintConfigPrettier from 'eslint-config-prettier'

export default [
  // 基础推荐规则
  js.configs.recommended,

  // Vue 推荐规则
  ...vue.configs['flat/recommended'],

  // TypeScript 配置
  {
    files: ['**/*.ts', '**/*.tsx'],
    languageOptions: {
      parser: tsparser,
      parserOptions: {
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
    plugins: {
      '@typescript-eslint': tseslint,
    },
    rules: {
      ...tseslint.configs.recommended.rules,
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/no-explicit-any': 'warn',
    },
  },

  // JavaScript 配置
  {
    files: ['**/*.js', '**/*.mjs'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        // 浏览器全局变量
        window: 'readonly',
        document: 'readonly',
        navigator: 'readonly',
        console: 'readonly',
        setTimeout: 'readonly',
        setInterval: 'readonly',
        clearTimeout: 'readonly',
        clearInterval: 'readonly',
        fetch: 'readonly',
        URL: 'readonly',
        URLSearchParams: 'readonly',
        FormData: 'readonly',
        Blob: 'readonly',
        File: 'readonly',
        FileReader: 'readonly',
        localStorage: 'readonly',
        sessionStorage: 'readonly',
        // Node.js 全局变量（构建脚本）
        process: 'readonly',
        __dirname: 'readonly',
        __filename: 'readonly',
        module: 'readonly',
        require: 'readonly',
        exports: 'readonly',
        global: 'readonly',
        Buffer: 'readonly',
      },
    },
  },

  // Vue 文件配置
  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tsparser,
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
  },

  // 通用规则
  {
    rules: {
      // 代码质量
      'no-console': 'warn',
      'no-debugger': 'warn',
      'no-alert': 'warn',
      'no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      'no-undef': 'off', // 由 TypeScript 处理
      'prefer-const': 'warn',
      'no-var': 'error',

      // 代码风格（部分由 Prettier 处理）
      'quotes': ['warn', 'single', { avoidEscape: true }],
      'semi': ['warn', 'never'],
      'comma-dangle': ['warn', 'always-multiline'],
      'indent': ['warn', 2, { SwitchCase: 1 }],

      // Vue 特定规则
      'vue/multi-word-component-names': 'off', // 允许单词组件名
      'vue/no-v-html': 'warn',
      'vue/component-definition-name-casing': ['warn', 'PascalCase'],
      'vue/html-indent': ['warn', 2],
      'vue/html-self-closing': ['warn', {
        html: { void: 'always', normal: 'always', component: 'always' },
      }],
    },
  },

  // 忽略 Prettier 冲突的规则
  eslintConfigPrettier,

  // 忽略的文件/目录
  {
    ignores: [
      'node_modules/**',
      'dist/**',
      'artifacts/**',
      'build.mjs',
      '*.config.js',
      '*.config.ts',
      '__tests__/**',
    ],
  },
]
