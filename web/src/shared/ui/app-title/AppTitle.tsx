import styles from './AppTitle.module.css';

type AppTitleProps = {
  title?: string;
};

export function AppTitle({ title }: AppTitleProps) {
  const label = title === undefined || title.trim() === '' ? 'XFlickr' : title;

  return (
    <h1 className={styles.title}>
      <span className={styles.mark} aria-hidden="true" />
      {label}
    </h1>
  );
}
