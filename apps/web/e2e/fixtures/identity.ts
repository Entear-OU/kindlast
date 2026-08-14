import { execFile } from 'node:child_process'
import path from 'node:path'
import { promisify } from 'node:util'

const run = promisify(execFile)

/**
 * Creating a person in the identity provider, without sending any mail.
 *
 * The signup journey normally ends at a verification code, which is exactly
 * the half a browser test cannot drive. Zitadel's import endpoint takes the
 * verified flag and the password in the same call, so a fixture user arrives
 * already able to sign in. That is not a shortcut around the product's real
 * behaviour: verification is Zitadel's concern and Zitadel's test surface, and
 * what this repository owns is everything after the redirect comes back.
 *
 * Mail does now work on this stack, so this is a choice about speed and blast
 * radius rather than a workaround. A journey test that waited for a message
 * would depend on Mailpit, on Zitadel's notifier, and on delivery timing, none
 * of which is the code under test, and would fail for reasons that teach
 * nothing about this repository.
 *
 * The reason mail used to go nowhere is worth keeping, because the error names
 * the wrong thing: Zitadel refuses an SMTP provider that has no credentials
 * and reports it as `SMTPConfig.NotFound`, while its own API and projection
 * both show the config present and active (zitadel/zitadel#8344). The seed job
 * now sets credentials Mailpit does not need for exactly this reason.
 */

const ZITADEL_URL = process.env.KINDLAST_AUTH_URL ?? 'http://localhost:8300'
// Absolute, because docker resolves -f against the working directory and this
// file sits four levels below the repository root.
const COMPOSE_FILE = path.resolve(__dirname, '../../../../deploy/compose.yaml')

/**
 * A development-only credential for throwaway users this fixture creates and
 * deletes. It exists in the repository for the same reason the compose
 * passwords do: it is a local value with no reach beyond a disposable stack.
 */
export const FIXTURE_PASSWORD = 'Passw0rd!-e2e-fixture'

export interface FixtureUser {
  id: string
  username: string
  email: string
}

/**
 * The seed bot's personal access token, which Zitadel writes into a named
 * volume at first-instance setup. There is no host-side path to it, so the
 * only way in is a container that mounts the same volume.
 */
let cachedToken: string | undefined

async function seedBotToken(): Promise<string> {
  if (cachedToken) return cachedToken

  const { stdout } = await run(
    'docker',
    [
      'compose',
      '-f',
      COMPOSE_FILE,
      'run',
      '--rm',
      '--no-deps',
      '-T',
      '--entrypoint',
      'sh',
      'seed',
      '-c',
      'cat /machinekey/seed-bot-pat.txt',
    ],
  )

  const token = stdout.trim()
  if (!token) {
    throw new Error(
      'the seed bot PAT was empty; is the compose stack up? ' +
        'docker compose -f deploy/compose.yaml up -d',
    )
  }

  cachedToken = token
  return token
}

async function management(method: string, path: string, body?: unknown) {
  const response = await fetch(`${ZITADEL_URL}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${await seedBotToken()}`,
      'Content-Type': 'application/json',
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (!response.ok) {
    throw new Error(`${method} ${path} -> ${response.status}: ${await response.text()}`)
  }

  return response.json()
}

/**
 * A human who can sign in immediately: email already verified, password
 * already set, and no change demanded on first use. All three matter, because
 * any one of them left at its default parks the browser on a Zitadel screen
 * this test has no business driving.
 */
export async function createVerifiedUser(label: string): Promise<FixtureUser> {
  const unique = `${label}-${Date.now()}-${Math.floor(Math.random() * 1e6)}`
  const email = `${unique}@kindlast.test`

  const created = await management('POST', '/management/v1/users/human/_import', {
    userName: email,
    profile: { firstName: 'Fixture', lastName: label },
    email: { email, isEmailVerified: true },
    password: FIXTURE_PASSWORD,
    passwordChangeRequired: false,
  })

  return { id: created.userId, username: email, email }
}

/**
 * Count organisations, out of band.
 *
 * Deliberately as the superuser rather than through the application's role.
 * Every table forces row level security, so `kindlast_app` and even the
 * schema owner see nothing without the two session GUCs set, and an assertion
 * that silently counted zero would report idempotence it never checked. A
 * superuser bypasses RLS, which is precisely wrong for the application and
 * exactly right for a test that wants the unfiltered truth.
 */
export async function countOrganisations(): Promise<number> {
  const { stdout } = await run('docker', [
    'compose',
    '-f',
    COMPOSE_FILE,
    'exec',
    '-T',
    '-e',
    `PGPASSWORD=${process.env.KINDLAST_PG_SUPER_PASSWORD ?? 'postgres-dev-password'}`,
    'postgres-app',
    'psql',
    '-U',
    'postgres',
    '-d',
    'kindlast',
    '-tAc',
    'select count(*) from organisations',
  ])

  const count = Number(stdout.trim())
  if (!Number.isInteger(count)) {
    throw new Error(`could not read an organisation count, got: ${stdout}`)
  }
  return count
}

export async function deleteUser(id: string): Promise<void> {
  try {
    await management('DELETE', `/management/v1/users/${id}`)
  } catch {
    // A fixture that fails to clean up must not fail the run it belongs to.
    // The users are disposable and the stack is torn down with `down -v`.
  }
}
