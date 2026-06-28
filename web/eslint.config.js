import svelte from 'eslint-plugin-svelte';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  ...tseslint.configs.recommended,
  ...svelte.configs['flat/recommended'],
  {
    // svelte-eslint-parser handles *.svelte; we need TS parser for *.svelte.ts
    // (Svelte 5 reactive modules — not component files, but they use $state etc.)
    files: ['**/*.svelte.ts'],
    languageOptions: {
      parser: tseslint.parser,
    },
  },
  {
    files: ['**/*.svelte'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
      },
    },
  },
  {
    rules: {
      // Allow _ prefix to suppress unused-variable warnings.
      '@typescript-eslint/no-unused-vars': ['error', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
        caughtErrorsIgnorePattern: '^_',
      }],
      // Svelte 5 rune calls ($effect, $derived) look like void expressions to TS.
      '@typescript-eslint/no-unused-expressions': 'off',
      // Empty interfaces are common in tygo-generated type files and for nominal
      // typing. Disabling globally avoids noise on generated code.
      '@typescript-eslint/no-empty-object-type': 'off',
      // SvelteDate/SvelteMap/SvelteSet migration is nice-to-have, not a blocker.
      'svelte/prefer-svelte-reactivity': 'warn',
      // Stale svelte-ignore comments — warn rather than block CI.
      'svelte/no-unused-svelte-ignore': 'warn',
      // {#each} key expressions are good practice but enforcing on existing code
      // would require auditing all callers; address in a dedicated cleanup pass.
      'svelte/require-each-key': 'off',
    },
  },
  {
    ignores: ['dist/**', 'node_modules/**'],
  },
);
