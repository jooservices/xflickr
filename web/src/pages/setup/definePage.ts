import type { SetupPage, SetupPageDefinition } from './types';

/** Fills the page contract defaults while keeping every runtime member required. */
export function definePage(definition: SetupPageDefinition): SetupPage {
  const {
    title,
    onFail = definition.id,
    rollbackTo = [],
    showRollback = () => true,
    visibleInProgress = true,
  } = definition;
  return {
    ...definition,
    title: typeof title === 'function' ? title : () => title,
    onFail,
    rollbackTo,
    showRollback,
    visibleInProgress,
  };
}
