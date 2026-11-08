import type { RequestHandler } from './$types';
import { brandRedirect } from '#lib/server/brand.js';

// The brand files live in the media bucket; send browsers to the current one.
export const GET: RequestHandler = ({ fetch }) => brandRedirect(fetch, 'favicon_svg');
