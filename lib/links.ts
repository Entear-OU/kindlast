/**
 * Outbound links that appear in more than one surface.
 *
 * The repository went public under AGPL-3.0 in ENT-175, so the GitHub URL is
 * now marketing copy as much as it is a developer link: it shows up in the
 * public header, the footer, and the open-source section. Keeping it in one
 * place means a rename or an org move is a single edit.
 */

/**
 * Canonical casing as GitHub reports it (`gh repo view`), not the lowercase
 * form in `git remote`. GitHub redirects case-insensitively so either resolves,
 * but the handle is rendered as copy on the landing page and should match.
 *
 * NOTE: the repository is still private as of this commit. These links 404 for
 * logged-out visitors until it is flipped to public (ENT-177).
 */
export const GITHUB_REPO_URL = 'https://github.com/Entear-OU/kindlast'

/** The `owner/name` handle, rendered verbatim on the open-source repo card. */
export const GITHUB_REPO_HANDLE = 'Entear-OU/kindlast'

export const GITHUB_LICENSE_URL = `${GITHUB_REPO_URL}/blob/main/LICENSE`

/** SPDX identifier, rendered verbatim in the footer and the repo card. */
export const LICENSE_SPDX = 'AGPL-3.0'
