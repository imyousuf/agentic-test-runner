import { useEffect, useState } from 'react';

export type Route =
  | { view: 'live' }
  | { view: 'library' }
  | { view: 'player'; id: string };

/**
 * parseRoute reads the hash. The hash is the whole router, because the live
 * view is served from one HTML file and a path-based route would need the Go
 * server to serve that file for every unknown path.
 */
export function parseRoute(hash: string): Route {
  const clean = hash.replace(/^#\/?/, '');
  if (clean === 'recordings') return { view: 'library' };
  const play = /^recordings\/([^/]+)$/.exec(clean);
  if (play) return { view: 'player', id: play[1] };
  return { view: 'live' };
}

export function useRoute(): [Route, (to: string) => void] {
  const [route, setRoute] = useState<Route>(() => parseRoute(location.hash));

  useEffect(() => {
    const onHash = () => setRoute(parseRoute(location.hash));
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);

  return [route, (to: string) => { location.hash = to; }];
}
