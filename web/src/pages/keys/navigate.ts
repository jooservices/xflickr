/** Leaves the app for an external page (Flickr's authorize page). Isolated so tests can stub it. */
export function leaveTo(url: string): void {
  window.location.assign(url);
}
