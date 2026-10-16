import { useEffect } from "react";

const set = (sel: string, attr: string, value: string) => document.head.querySelector(sel)?.setAttribute(attr, value);

/** Per-page title, description and canonical for the public pages (the server adds the origin to the static ones). */
export function useSeo(title: string, description: string, path: string) {
  useEffect(() => {
    document.title = title;
    const url = location.origin + path;
    set('meta[name="description"]', "content", description);
    set('link[rel="canonical"]', "href", url);
    set('meta[property="og:title"]', "content", title);
    set('meta[property="og:description"]', "content", description);
    set('meta[property="og:url"]', "content", url);
    set('meta[name="twitter:title"]', "content", title);
    set('meta[name="twitter:description"]', "content", description);
  }, [title, description, path]);
}
