import type { ReactNode } from 'react';
import { glossaryTerms } from './init';

type TermProps = {
  /** Product term that must stay literal in every language. */
  children: string;
  className?: string;
};

const termSet = new Set(glossaryTerms.map((term) => term.toLowerCase()));

/** Renders a glossary term unchanged; warns in development if it is not listed. */
export function Term({ children, className }: TermProps) {
  if (import.meta.env.DEV && !termSet.has(children.toLowerCase())) {
    console.warn(`i18n: "${children}" is not in glossary.json`);
  }
  return (
    <span className={className} data-glossary-term={children}>
      {children}
    </span>
  );
}

/** True when a value is a listed glossary term (case-insensitive). */
export function isGlossaryTerm(value: string): boolean {
  return termSet.has(value.toLowerCase());
}

/** Pass-through for DATA values (genre names, tags, titles) — never translate. */
export function DataText({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span className={className} data-i18n-data="true">
      {children}
    </span>
  );
}
