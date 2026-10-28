const allowedTypes = [
  'feat',
  'fix',
  'docs',
  'style',
  'refactor',
  'perf',
  'test',
  'build',
  'ci',
  'chore',
  'revert',
];

// Keep the same format for commit headers and PR titles.
// The subject must start uppercase, must not end with a period, and must carry a Jira key.
const headerPattern = new RegExp(
  `^(${allowedTypes.join('|')})\\(([^()\\r\\n]+)\\): ([A-Z](?:.*[^.])?) \\(([A-Z][A-Z0-9]*-\\d+)\\)$`,
  'u',
);

export default {
  parserPreset: {
    parserOpts: {
      headerPattern,
      headerCorrespondence: ['type', 'scope', 'subject', 'ticket'],
    },
  },
  rules: {
    'header-max-length': [2, 'always', 120],
    'type-enum': [2, 'always', allowedTypes],
    'type-empty': [2, 'never'],
    'type-case': [2, 'always', 'lower-case'],
    'scope-empty': [2, 'never'],
    'subject-empty': [2, 'never'],
    'subject-case': [0],
  },
};
