import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'
import vueParser from 'vue-eslint-parser'

/**
 * Soft ESLint baseline for Vue + TS. Essential/recommended only;
 * no type-aware rules (avoids rewriting the whole app).
 */
export default tseslint.config(
  { ignores: ['dist/**', 'node_modules/**', '*.config.*'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/essential'],
  {
    files: ['src/**/*.{ts,vue}'],
    languageOptions: {
      parser: vueParser,
      parserOptions: {
        parser: tseslint.parser,
        ecmaVersion: 'latest',
        sourceType: 'module',
        extraFileExtensions: ['.vue'],
      },
    },
    rules: {
      'vue/multi-word-component-names': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      'no-unused-vars': 'off',
      // TS / vue-tsc already check undefined identifiers; browser globals trip no-undef in .vue scripts.
      'no-undef': 'off',
      'no-console': 'off',
      'prefer-const': 'warn',
      'no-empty': ['warn', { allowEmptyCatch: true }],
    },
  },
)
