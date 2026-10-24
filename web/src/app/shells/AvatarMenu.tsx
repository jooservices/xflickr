import { useRef, useState } from 'react';
import { Link } from 'react-router';
import { Icon, useI18n } from '../../shared';
import { useOverlay } from './useOverlay';
import styles from './shells.module.css';

type AvatarMenuProps = {
  name: string;
  email: string;
  onShortcuts: () => void;
  onSignOut: () => void;
};

/** Avatar button and its menu: identity, Profile, accounts, sessions, shortcuts, sign out. */
export function AvatarMenu({ name, email, onShortcuts, onSignOut }: AvatarMenuProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const close = () => {
    setOpen(false);
  };
  const openerRef = useRef<HTMLButtonElement>(null);
  const ref = useOverlay<HTMLDivElement>({
    open,
    onClose: close,
    closeOnOutside: true,
    openerRef,
  });
  const initials = (name || email).slice(0, 2).toUpperCase();

  return (
    <div className={styles.anchor}>
      <button
        ref={openerRef}
        type="button"
        className={`${styles.iconBtn} ${styles.hideSm}`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => {
          setOpen(!open);
        }}
        data-testid="shell-avatar-button"
      >
        <span className={styles.avatar} aria-hidden="true">
          {initials}
        </span>
        <span className={styles.srOnly}>{t('shell.avatar.button')}</span>
      </button>
      {open ? (
        <div
          ref={ref}
          className={styles.menu}
          role="menu"
          aria-label={t('shell.avatar.button')}
          data-testid="shell-avatar-menu"
        >
          <p className={styles.menuHeader} data-testid="shell-avatar-header">
            {t('shell.avatar.header', { name, email })}
          </p>
          <Link role="menuitem" to="/profile" onClick={close} data-testid="shell-avatar-profile">
            <Icon name="user" /> {t('shell.avatar.profile')}
          </Link>
          <Link role="menuitem" to="/accounts" onClick={close} data-testid="shell-avatar-accounts">
            <Icon name="link" /> {t('shell.avatar.accounts')}
          </Link>
          <Link role="menuitem" to="/sessions" onClick={close} data-testid="shell-avatar-sessions">
            <Icon name="shield" /> {t('shell.avatar.sessions')}
          </Link>
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              close();
              onShortcuts();
            }}
            data-testid="shell-avatar-shortcuts"
          >
            <Icon name="gear" /> {t('shell.avatar.shortcuts')}
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              close();
              onSignOut();
            }}
            data-testid="shell-avatar-sign-out"
          >
            <Icon name="logout" /> {t('shell.avatar.sign-out')}
          </button>
        </div>
      ) : null}
    </div>
  );
}
