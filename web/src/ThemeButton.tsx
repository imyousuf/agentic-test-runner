import { Icon } from './Icon';
import { useTheme, type ThemeChoice } from './useTheme';

const ORDER: ThemeChoice[] = ['auto', 'light', 'dark'];
const NAME: Record<ThemeChoice, string> = {
  auto: 'system theme',
  light: 'light theme',
  dark: 'dark theme',
};

/** ThemeButton cycles system → light → dark. */
export function ThemeButton() {
  const [theme, setTheme] = useTheme();
  const next = ORDER[(ORDER.indexOf(theme) + 1) % ORDER.length];
  const label = `Using the ${NAME[theme]}. Switch to the ${NAME[next]}.`;

  return (
    <button
      type="button"
      className="btn btn-icon"
      title={label}
      aria-label={label}
      onClick={() => setTheme(next)}
    >
      <Icon name={theme} />
    </button>
  );
}
