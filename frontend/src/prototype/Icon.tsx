export type IconName = 'overview' | 'server' | 'proxy' | 'topology' | 'users' | 'subscription' | 'traffic' | 'settings' | 'menu' | 'plus' | 'arrow' | 'help' | 'reset' | 'chevron' | 'grip' | 'check' | 'pause' | 'offline' | 'copy';

const paths: Record<IconName, string> = {
  check: 'M20 6 9 17l-5-5',
  pause: 'M8 5v14 M16 5v14',
  offline: 'M18 6 6 18 M6 6l12 12',
  copy: 'M9 9h11v12H9z M15 9V3H3v12h6',
  overview: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
  server: 'M4 3h16v7H4z M4 14h16v7H4z M7 6.5h.01 M7 17.5h.01 M11 6.5h6 M11 17.5h6',
  proxy: 'M12 3 3 8l9 5 9-5-9-5z M3 12l9 5 9-5 M3 16l9 5 9-5',
  topology: 'M3 3h6v6H3z M15 15h6v6h-6z M15 3h6v6h-6z M9 6h6 M6 9v9h9',
  users: 'M16 21v-3a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v3 M13 3a4 4 0 0 1 0 8 M22 21v-3a4 4 0 0 0-3-3.8 M9 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8z',
  subscription: 'M4 3h11l5 5v13H4z M14 3v6h6 M8 13h8 M8 17h5',
  traffic: 'M3 3v18h18 M6 15l4-5 4 3 6-8',
  settings: 'M9 3h6l1 3 3 1 2 5-2 5-3 1-1 3H9l-1-3-3-1-2-5 2-5 3-1 1-3z M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z',
  menu: 'M4 6h16 M4 12h16 M4 18h16',
  plus: 'M12 5v14 M5 12h14',
  arrow: 'M4 12h16 M14 6l6 6-6 6',
  help: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z M9.5 9a2.5 2.5 0 1 1 4 2l-1.5 1v1 M12 16h.01',
  reset: 'M3 4v6h6 M3 10a9 9 0 1 1 1 7',
  chevron: 'm9 5 7 7-7 7',
  grip: 'M8 5h.01 M16 5h.01 M8 12h.01 M16 12h.01 M8 19h.01 M16 19h.01',
};

export function Icon({ name, size = 18 }: { name: IconName; size?: number }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={paths[name]} /></svg>;
}
