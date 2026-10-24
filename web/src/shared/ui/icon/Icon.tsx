import { ICON_PATHS } from './paths';
import type { IconName } from './paths';

type IconProps = {
  name: IconName;
  size?: number;
};

/** Decorative icon: the adjacent text or the control's aria-label names the action. */
export function Icon({ name, size = 18 }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      dangerouslySetInnerHTML={{ __html: ICON_PATHS[name] }}
    />
  );
}
