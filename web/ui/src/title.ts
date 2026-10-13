import { useEffect } from 'react';
import { useMatches, type Params } from 'react-router';

export const APP_TITLE = 'authentik bench';

/** RouteTitle is what a route puts in its `handle.title`: a fixed string, or
 * one built from the route's params (e.g. the object's name). */
export type RouteTitle = string | ((params: Params) => string);

type Match = { params: Params; handle: unknown };

/** pageTitle picks the deepest route that declares a title and formats it as
 * "<page> · authentik bench". Routes without one fall back to the app name. */
export function pageTitle(matches: Match[]): string {
  for (let i = matches.length - 1; i >= 0; i--) {
    const title = (matches[i].handle as { title?: RouteTitle } | undefined)?.title;
    if (title !== undefined) {
      const text = typeof title === 'function' ? title(matches[i].params) : title;
      return text ? `${text} · ${APP_TITLE}` : APP_TITLE;
    }
  }
  return APP_TITLE;
}

/** useDocumentTitle keeps the browser tab title in sync with the open page. */
export function useDocumentTitle() {
  const title = pageTitle(useMatches());
  useEffect(() => {
    document.title = title;
  }, [title]);
}
